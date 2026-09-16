# server/Makefile —— 服务端构建辅助目标（round 38 S6-1 起入库）。
# 注意：仓库根 .gitignore 的 CMake 模板规则会忽略裸名 Makefile，
# 已在 .gitignore 加 !server/Makefile 例外，勿删。
# Windows Git Bash 兼容：仅用 rm/mkdir/touch/cp，无 find -delete 等 GNU 扩展。

.PHONY: web-dist openapi-lint serve-db

# openapi-lint：redocly 校验契约唯一事实来源（TODO.md §0.2 验证命令）。
openapi-lint:
	cd ../api && npx @redocly/cli lint openapi.yaml

# serve-db：起开发 PostgreSQL（docker compose，端口 5433）。goose 迁移由
# server 启动时自动执行（store.Open 内嵌 goose up，幂等；ASTRAL_AUTO_MIGRATE
# 开关见 config）。单独跑迁移的运维路径见 docs/deployment.md §14（goose CLI）。
serve-db:
	cd .. && docker compose up -d

# web-dist：构建 web 前端并整树拷入 server/webdist/dist（TODO.md S6-1）。
# go:embed 在编译期嵌入——本目标之后（重新）go build 的产物即同源托管前端。
# 先清空 webdist/dist 再拷贝（保留 .gitkeep 占位：go:embed 不支持空目录），
# 避免陈旧哈希资产堆积；Vite 产物含 index.html 与 assets/ 两类。
web-dist:
	cd ../web && npm run build
	rm -rf webdist/dist
	mkdir webdist/dist
	touch webdist/dist/.gitkeep
	cp -r ../web/dist/. webdist/dist/
