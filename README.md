# 纸安批次放行台

纸安批次放行台面向纸质档案保护实验室，提供单一版本化 JSON HTTP API。系统把脱酸处理批次从处理前基线冻结、逐件测量、质量判定、异常纠正复测、独立抽检推进到不可再写的放行或拒绝终态，并保存原子快照、摘要链时间线和规范 JSON 证书。

服务不依赖外部数据库。默认数据写入 `./paperqual-data`，监听 `127.0.0.1:19081`。可通过 `-addr=127.0.0.1:<port>` 指定回环监听地址，也可通过 `PORT` 指定端口号；服务不会监听外网地址。

## 构建

```sh
go build ./...
```

## 运行

```sh
go run ./cmd/paperqual -addr=127.0.0.1:19081
```

所有写请求使用 `application/json`，请求体包含 `request_id` 和 `expected_revision`，操作者身份通过 `X-Actor-ID` 传入。主要资源位于 `/api/v1/batches`，健康检查位于 `GET /ready`。

## 扩展业务入口

- `POST /api/v1/batches/{batch_id}/items/batch`：原子登记 `items`，每次 1 到 100 件。全部项目通过领域校验后只提升一个修订号并产生一个 `items.batch_registered` 事件；重复 `request_id` 原样重放首次响应。失败响应的 `error.details` 包含从 0 开始的 `index`、`item_id` 和稳定 `code`，不会保存部分项目。
- `POST /api/v1/batches/{batch_id}/treatment-rounds/preflight`：请求体与正式轮次入口一致。响应包含 `current_revision`、冻结基线摘要、逐件失败码、`overall_result` 和 `expected_status`；预检不写轮次、事件或幂等索引。
- `POST /api/v1/batches/{batch_id}/corrections/batch`：原子登记覆盖全部待纠正异常件的 `corrections`。每项 `reason` 使用 `{"category":"alkaline_reserve","description":"药液浓度不足"}` 结构，`category` 可为 `surface_ph`、`alkaline_reserve` 或 `color_delta_e`，并须与该件原始失败码匹配。

时间线入口 `GET /api/v1/batches/{batch_id}/timeline` 支持 `cursor`、`limit`（1 到 100）、`event_type`、`actor_id`、`min_revision`、`max_revision` 和 `snapshot_anchor`。响应中的 `event_anchor` 是完整已核验事件链的查询快照标识，后续页应把它作为 `snapshot_anchor` 传回；查询期间新增事件会返回 `timeline_changed`，不会把不同锚点的页面拼接在一起。`page_size`、`actor`、`revision_from`、`revision_to` 和 `anchor` 分别是上述条件的兼容别名，同一条件不能混用两个名称。

可运行真实 HTTP 自检。自检使用临时数据目录，完成一次合格放行、校验证书和事件链后自行退出：

```sh
go run ./cmd/paperqual -self-check -addr=127.0.0.1:19081
```

## 测试

```sh
go test ./...
```
