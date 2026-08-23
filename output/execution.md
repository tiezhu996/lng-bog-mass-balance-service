# gb-529 执行与验收记录

- 项目：LNG 蒸发气物理质量平衡分析
- 验收日期：2026-08-22（Asia/Shanghai）
- 实现提交：`65d56c1f179212489a98ad0eb0bf78636515cd29`
- 安全边界：系统只提供离线质量平衡分析和人工复核，不连接现场仪表、阀门、PLC 或控制系统，也不自动下发生产指令。

## 结论

项目通过 Go 构建、静态检查和单元测试，前端生产构建、规模门禁、SQLite 启动冒烟、PostgreSQL Compose 三服务验证、30 项真实 API 断言和 Codex 内置 Browser 桌面/移动交互验收。验收覆盖储罐、测量快照、转运、质量平衡、接受/驳回及追加式审计主流程。最终镜像中的 ECharts 图表正常渲染，Browser 控制台无错误；验收后已删除本项目容器、网络和数据卷。

## 构建、测试与规模

| 命令 | 结果 |
| --- | --- |
| `go work sync` | 通过，工作区依赖可解析 |
| `go build ./backend/...` | 通过 |
| `go vet ./backend/...` | 通过 |
| `go test ./backend/...` | 通过；质量平衡、状态约束和中间件测试通过 |
| `npm --prefix frontend ci` | 通过，锁文件可复现安装 |
| `npm --prefix frontend run build` | 通过，TypeScript 类型检查和 Vite 生产构建完成 |
| `npm audit --registry=https://registry.npmjs.org --omit=dev --audit-level=high` | 生产依赖 0 个已报告漏洞 |
| 规模检查 | 3330 行功能 Go 代码 / 42 个功能 `.go` 文件，符合 2500-4200 行和 24-42 文件红线 |
| SQLite runtime smoke | `go run ./cmd/server` 启动后，`http://127.0.0.1:20529/healthz` 返回 200 |
| `docker compose config --quiet` | 通过，中文目录下 Compose 名解析正常 |

实现阶段修复了两处会阻断真实验收的问题：

- 将 TypeScript 从 5.5.4 更新到 5.9.3，使 `@vitejs/plugin-react 6.1.0` 的类型声明可由锁定工具链稳定解析；重新执行 `npm ci && npm run build` 通过，并重建前端镜像回归。
- 为 `EstimatedBOGKG` 显式指定 GORM 列名 `estimated_bog_kg`，保证 SQLite runtime smoke 与 PostgreSQL 生产路径使用一致字段映射。

## Compose 与健康检查

执行 `docker compose up -d --build` 后：

| 服务 | 映射端口 | 最终状态 |
| --- | --- | --- |
| `lng-boiloff-gas-balance-db` | `57529 -> 5432` | `healthy` |
| `lng-boiloff-gas-balance-backend` | `19529 -> 8080` | `healthy` |
| `lng-boiloff-gas-balance-frontend` | `18529 -> 80` | `healthy` |

真实探针：

- `GET http://127.0.0.1:19529/healthz` -> 200，服务状态为 `ok`。
- `GET http://127.0.0.1:19529/readyz` -> 200，数据库状态为 `ok`。
- `GET http://127.0.0.1:18529/api/v1/healthz` -> 200，证明 Nginx 反向代理可用。
- 最终三服务日志未发现意外 4xx/5xx、panic 或前端资源加载失败。

## PostgreSQL API 验收

同一 Compose PostgreSQL 实例上执行 30 项断言，全部通过：

1. 直连和前端反代的健康/就绪探针均返回 200。
2. 未认证读取储罐返回 401 `AUTH_REQUIRED`；分析员和复核员登录、身份读取均成功。
3. 储罐创建返回 201，列表和详情可读取；重复或不合法业务输入由统一错误结构返回。
4. 缺少期初边界快照时运行被 422 `OPENING_SNAPSHOT_MISSING` 阻断。
5. 期初/期末测量快照、流入和流出转运均真实写入；时间重叠转运返回 409 `TRANSFER_TIME_OVERLAP`。
6. 测量质量、储罐测量和转运查询均返回真实 PostgreSQL 数据。
7. 可接受路径的质量平衡创建、提交复核和不确定度分解均成功。
8. reviewer 尝试分析员写操作返回 403 `ACCESS_DENIED`。
9. reviewer 接受一条平衡、驳回另一条平衡均成功，列表反映最终状态。
10. 审计查询返回本轮储罐、快照、转运、平衡和复核动作的追加式记录。

终端断言总结为 `API_SMOKE_PASS`，30/30 项通过；最后一次运行生成 accepted balance #5、rejected balance #6 和 24 条审计记录。

## Codex 内置 Browser 验收

仅使用 Codex 内置 Browser，未使用外部 Chrome、Computer Use 或独立 Playwright。验收覆盖桌面视口和 390 x 844 移动视口。

- process analyst 登录后，储罐、测量、转运、平衡和审计页面均显示真实 API 数据。
- 在 UI 创建 `BROWSER-529` 储罐成功，列表数量由 3 增至 4；后端 `POST /api/v1/tanks` 返回 201。
- 分析员可运行平衡并提交复核；复核员可切换角色查看队列并执行 Accept / Reject，状态和证据即时更新。
- 接受结果展示质量守恒分解、不确定度区间和 ECharts 图表；最终生产镜像中检测到 1 个真实 canvas，图表非空。
- 审计记录可展开，显示 request ID、动作、实体以及 before/after 摘要。
- 390 x 844 下导航、表格横向滚动、表单和主操作按钮均可使用，无文字遮挡或控件重叠。
- 最终 `dev.logs()=[]`；Browser 期间未观察到意外 4xx/5xx、脚本异常或主流程阻断。

截图：

- `output/browser-tank-create.jpg`
- `output/browser-balance-accepted.jpg`
- `output/browser-audit-expanded.jpg`
- `output/browser-balance-desktop-viewport.jpg`
- `output/browser-balance-mobile-viewport.jpg`

## 停服与清理

执行：

```bash
docker compose down -v --remove-orphans
```

清理结果：

- `lng-boiloff-gas-balance-frontend`、`backend`、`db` 三个容器均 stopped/removed。
- `lng-boiloff-gas-balance_postgres_data` 命名卷已删除。
- `lng-boiloff-gas-balance_default` 网络已删除。
- `docker compose ps -a` 无服务。
- 按 `lng-boiloff-gas-balance` 过滤的 container/volume/network 查询结果均为空，未影响其他项目。
