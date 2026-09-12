// Package bootstrap 实现 astral-server 的交互式初始化向导（cmd/astral-bootstrap）。
//
// 它把原先的手工 bootstrap（cp .env.example 并手改 → 启动服务自动迁移 →
// curl POST /auth/register 建首个账号）收敛为单条命令：
// 配置问答 → 连库实测 → goose 迁移 → 固化 server_id → 写 server/.env →
// 创建首个 human 管理员。除最终的 .env 写回（先备份 .env.bak）外不落任何
// 盘面变更；各步骤幂等，重复运行安全（goose up 幂等、server_id 库值优先、
// 已有 human 时跳过创建）。
//
// 业务规则不在此重复实现：连接/迁移/身份复用 internal/store，账号创建直接
// 调用 auth.Service.Register（含口令强度校验与「已有 human 即关闭注册」守卫）。
package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/joho/godotenv"
	"github.com/pressly/goose/v3"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/config"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/auth"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/store"
)

// exampleDSN 与 docker-compose.yml 的开发库一致（postgres:17 @ 5433）。
const exampleDSN = "postgres://astral:astral@localhost:5433/astral?sslmode=disable"

// Run 执行向导主流程。stdin/stdout 抽象为 io 以便管道驱动与测试；
// 应在 server/ 目录下运行（.env 与 godotenv.Load 同为 CWD 相对）。
func Run(stdin io.Reader, stdout io.Writer) error {
	ui := NewUI(stdin, stdout)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// 提示阶段的阻塞读无法被 ctx 打断；收到真实信号时直接退出（此时未写任何
	// 文件）。不能用 NotifyContext：其 stop() 也会关闭 Done，与真实信号无法区分。
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		select {
		case <-sigCh:
			fmt.Fprintln(stdout, "\naborted")
			os.Exit(130)
		case <-ctx.Done(): // 正常返回路径，静默退出
		}
	}()

	fmt.Fprintln(stdout, "=== astral-server bootstrap 向导 ===")
	fmt.Fprintln(stdout, "流程：配置问答 → 验证数据库 → goose 迁移 → 固化 server_id → 写 .env → 创建首个管理员")
	fmt.Fprintln(stdout, "随时 Ctrl+C 退出；写入 .env 前不会改动任何文件。")
	if _, err := os.Stat("go.mod"); err != nil {
		fmt.Fprintln(stdout, "提示：当前目录没有 go.mod；建议在 server/ 目录下运行（.env 将写入当前目录）。")
	}
	fmt.Fprintln(stdout)

	// 现值作默认：与 cmd/astral-server 相同的 godotenv 预载语义（shell 环境变量优先）。
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(stdout, "警告：.env 解析失败（忽略）：%v\n", err)
	}
	cur := config.Load()
	// store/auth 内部日志全部静默（含 DSN 等），向导自行控制输出。
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))

	// ---- 数据库 DSN：问答 + 实测连接（失败可重试） ----
	dsnDef := cur.DatabaseDSN
	if dsnDef == "" {
		dsnDef = exampleDSN
	}
	dsn, err := ui.AskString("PostgreSQL DSN（回车用默认；输入 none = 无数据库桩模式）", dsnDef)
	if err != nil {
		return err
	}
	stub := isNone(dsn)
	if stub {
		ok, err := ui.AskBool("确认不配置数据库（桩模式：受保护端点 401 / readyz 503）？", false)
		if err != nil {
			return err
		}
		if !ok {
			stub = false
			if dsn, err = ui.AskString("PostgreSQL DSN", dsnDef); err != nil {
				return err
			}
		}
	}

	var gormDB *gorm.DB
	if !stub {
		for {
			if vErr := ValidateDSN(dsn); vErr != nil {
				fmt.Fprintf(stdout, "  ✗ %v\n", vErr)
			} else if db, oErr := store.Open(ctx, dsn, false, quiet); oErr == nil {
				gormDB = db
				// 静默 gorm 日志：EnsureServerID 首启的 record-not-found 属预期
				// miss，默认 logger 会以 Warn 打到 stderr 干扰向导输出。
				gormDB.Logger = gormlogger.Default.LogMode(gormlogger.Silent)
				fmt.Fprintln(stdout, "  ✓ 数据库连接成功")
				break
			} else {
				fmt.Fprintf(stdout, "  ✗ 连接失败：%v\n", oErr)
			}
			retry, err := ui.AskBool("重试连接？", true)
			if err != nil {
				return err
			}
			if !retry {
				return errors.New("数据库连接失败，向导中止（未写任何文件）")
			}
			if dsn, err = ui.AskString("PostgreSQL DSN（输入 none = 无数据库桩模式）", dsn); err != nil {
				return err
			}
			if stub = isNone(dsn); stub {
				break
			}
		}
	}

	// ---- 其余配置问答 ----
	addr, err := ui.AskString("HTTP 监听地址", cur.HTTPAddr)
	if err != nil {
		return err
	}
	publicURL, err := ui.AskString("Public URL（canonical URL，可空）", cur.PublicURL)
	if err != nil {
		return err
	}
	serverIDIn, err := ui.AskString("server_id（留空自动生成；仅首启生效，之后以库中值为准）", cur.ServerID)
	if err != nil {
		return err
	}
	autoMigrate, err := ui.AskBool("服务启动时自动 goose up（生产多实例部署应关闭）", cur.AutoMigrate)
	if err != nil {
		return err
	}
	logLevel, err := ui.AskSelect("日志级别", []string{"debug", "info", "warn", "error"}, logLevelIndex(cur.LogLevel))
	if err != nil {
		return err
	}
	cors, err := ui.AskString("开发期 CORS 白名单（逗号分隔，可空）", strings.Join(cur.DevCORSOrigins, ","))
	if err != nil {
		return err
	}

	// ---- 数据库步骤：迁移 + 固化 server_id ----
	var serverID string
	if !stub {
		sqlDB, err := gormDB.DB()
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, "\n→ 执行 goose 迁移（幂等）…")
		if err := store.Migrate(ctx, sqlDB); err != nil {
			return fmt.Errorf("迁移失败：%w", err)
		}
		if v, err := goose.GetDBVersionContext(ctx, sqlDB); err == nil {
			fmt.Fprintf(stdout, "  ✓ schema 版本：%d\n", v)
		} else {
			fmt.Fprintln(stdout, "  ✓ 迁移完成")
		}

		serverID, err = store.EnsureServerID(ctx, gormDB, serverIDIn)
		if err != nil {
			return fmt.Errorf("固化 server_id 失败：%w", err)
		}
		if serverIDIn != "" && serverID != serverIDIn {
			fmt.Fprintf(stdout, "  ✓ server_id 沿用 server_meta 库中值 %s（输入的 %s 被忽略）\n", serverID, serverIDIn)
		} else {
			fmt.Fprintf(stdout, "  ✓ server_id = %s\n", serverID)
		}
	} else {
		serverID = serverIDIn
	}

	// ---- 确认并写 .env ----
	vals := EnvValues{
		HTTPAddr:       addr,
		PublicURL:      strings.TrimSpace(publicURL),
		DatabaseDSN:    dsn,
		ServerID:       serverID,
		AutoMigrate:    autoMigrate,
		DevCORSOrigins: strings.TrimSpace(cors),
		LogLevel:       logLevel,
	}
	fmt.Fprintln(stdout, "\n--- 配置确认 ---")
	fmt.Fprintf(stdout, "  数据库：%s\n", MaskDSN(vals.DatabaseDSN))
	fmt.Fprintf(stdout, "  HTTP：%s   日志：%s   自动迁移：%v\n", vals.HTTPAddr, vals.LogLevel, vals.AutoMigrate)
	if vals.PublicURL != "" {
		fmt.Fprintf(stdout, "  Public URL：%s\n", vals.PublicURL)
	}
	if vals.DevCORSOrigins != "" {
		fmt.Fprintf(stdout, "  CORS 白名单：%s\n", vals.DevCORSOrigins)
	}
	if vals.ServerID != "" {
		fmt.Fprintf(stdout, "  server_id：%s\n", vals.ServerID)
	}

	write, err := ui.AskBool("\n写入 "+envFile+"（已有文件先备份为 "+envFile+".bak）？", true)
	if err != nil {
		return err
	}
	if write {
		bak, err := WriteEnvFile(envFile, vals)
		if err != nil {
			return fmt.Errorf("写 %s 失败：%w", envFile, err)
		}
		if bak {
			fmt.Fprintf(stdout, "✓ 已写入 %s（原文件备份为 %s.bak）\n", envFile, envFile)
		} else {
			fmt.Fprintf(stdout, "✓ 已写入 %s\n", envFile)
		}
	} else {
		fmt.Fprintf(stdout, "已跳过写 %s\n", envFile)
	}

	// ---- 首个管理员（human）账号 ----
	adminEmail := ""
	adminExists := false
	if !stub {
		fmt.Fprintln(stdout, "\n→ 首个管理员账号")
		var humans int64
		if err := gormDB.WithContext(ctx).Model(&model.Actor{}).Where("kind = ?", "human").Count(&humans).Error; err != nil {
			return fmt.Errorf("查询 human 账号失败：%w", err)
		}
		if adminExists = humans > 0; adminExists {
			fmt.Fprintln(stdout, "  ✓ 已存在 human 账号（bootstrap 此前已完成），跳过创建")
		} else if ok, err := ui.AskBool("创建首个 human 管理员账号？", true); err != nil {
			return err
		} else if ok {
			adminEmail, err = createFirstHuman(ctx, ui, gormDB, quiet)
			if err != nil {
				return err
			}
		}
	}

	// ---- 总结 ----
	fmt.Fprintln(stdout, "\n=== bootstrap 完成 ===")
	fmt.Fprintf(stdout, "  数据库：%s\n", MaskDSN(vals.DatabaseDSN))
	fmt.Fprintf(stdout, "  HTTP：%s   日志：%s   自动迁移：%v\n", vals.HTTPAddr, vals.LogLevel, vals.AutoMigrate)
	if vals.ServerID != "" {
		fmt.Fprintf(stdout, "  server_id：%s\n", vals.ServerID)
	}
	if adminEmail != "" {
		fmt.Fprintf(stdout, "  管理员：%s\n", adminEmail)
	}
	if stub {
		fmt.Fprintln(stdout, "\n当前为桩模式；配置数据库后重跑本向导即可启用完整功能。")
	} else if adminEmail == "" && !adminExists {
		fmt.Fprintln(stdout, "\n尚未创建管理员；重跑本向导或参考 README 用 POST /api/v1/auth/register 创建。")
	}
	fmt.Fprintln(stdout, "\n下一步：")
	fmt.Fprintln(stdout, "  启动服务：cd server && go run ./cmd/astral-server")
	fmt.Fprintln(stdout, "  健康检查：curl http://127.0.0.1:8080/healthz")
	fmt.Fprintln(stdout, "  Web 控制台：用管理员账号登录（web 开发服务器见 README）")
	return nil
}

// createFirstHuman 交互式创建首个 human 账号，直接走 auth.Service.Register：
// 邮箱/口令校验、bcrypt、事务与「已有 human 即关闭」守卫都在 Service 内。
// 用户中途放弃不算错误（返回空邮箱）。返回创建的邮箱。
func createFirstHuman(ctx context.Context, ui *UI, db *gorm.DB, log *slog.Logger) (string, error) {
	email, err := ui.AskValid("管理员邮箱", "", ValidateEmail)
	if err != nil {
		return "", err
	}
	display, err := ui.AskString("显示名（回车取邮箱前缀）", strings.SplitN(email, "@", 2)[0])
	if err != nil {
		return "", err
	}
	svc := auth.NewService(db, log)
	for {
		pw, err := ui.AskSecret("密码（≥8 位，需同时含字母与数字）")
		if err != nil {
			return "", err
		}
		pw2, err := ui.AskSecret("确认密码")
		if err != nil {
			return "", err
		}
		if pw != pw2 {
			fmt.Fprintln(ui.Out, "  ✗ 两次输入不一致，请重来")
			continue
		}
		if vErr := ValidatePassword(pw); vErr != nil {
			fmt.Fprintf(ui.Out, "  ✗ %v\n", vErr)
			continue
		}
		actor, rErr := svc.Register(ctx, auth.RegisterInput{Email: email, Password: pw, DisplayName: display})
		if rErr == nil {
			fmt.Fprintf(ui.Out, "  ✓ 管理员已创建：%s（actor_id=%s）\n", email, actor.ID)
			return email, nil
		}
		var apiErr *httpx.APIError
		if !errors.As(rErr, &apiErr) {
			return "", fmt.Errorf("创建账号失败：%w", rErr)
		}
		fmt.Fprintf(ui.Out, "  ✗ %s\n", apiErr.Message)
		retry, err := ui.AskBool("重试？", true)
		if err != nil || !retry {
			if err != nil {
				return "", err
			}
			fmt.Fprintln(ui.Out, "  已跳过创建（重跑向导可再次尝试）")
			return "", nil
		}
	}
}

func isNone(s string) bool {
	return strings.EqualFold(strings.TrimSpace(s), "none")
}

func logLevelIndex(l slog.Level) int {
	switch l {
	case slog.LevelDebug:
		return 0
	case slog.LevelWarn:
		return 2
	case slog.LevelError:
		return 3
	default:
		return 1 // info
	}
}
