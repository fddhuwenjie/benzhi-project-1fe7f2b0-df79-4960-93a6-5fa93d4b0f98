# BENZHI_README

## 项目说明
- 项目：benzhi-project-1fe7f2b0-df79-4960-93a6-5fa93d4b0f98
- 项目用途：纸安批次放行台已完整实现纸质档案脱酸批次的基线冻结、逐件判定、异常纠正复测、独立抽检、终局封存和证据链验证流程。
- Go 工具链：`golang:1.22`
- 前端工具链：无

## 项目描述
- 项目名称：纸安批次放行台
- 项目介绍：面向纸质档案保护实验室的脱酸处理质量放行服务，将一个处理批次从处理前基线冻结、逐件测量、规则判定、超限返修、独立抽检推进到不可变放行证书或拒绝结论，保留可验证的完整证据链。
- 项目概述：面向纸质档案保护实验室的脱酸处理质量放行服务，将一个处理批次从处理前基线冻结、逐件测量、规则判定、超限返修、独立抽检推进到不可变放行证书或拒绝结论，保留可验证的完整证据链。
- 核心工作流：实验员创建脱酸处理批次并冻结纸张类型、目标酸碱度、最低碱储量和抽样规则，登记每件档案的处理前测量与证据摘要；处理后提交测量轮次，服务按冻结规则判定，超限项目进入隔离并在记录纠正措施和复测合格后解除；系统确定性生成独立抽检任务，复核员逐项签署，最终批准或拒绝批次并封存可验证证书，使批次进入不可再写的终态。
- 对外接口：仅提供版本化 JSON HTTP API：以 /api/v1/batches 及其 baseline、items、treatment-rounds、corrections、review、decision、timeline 和 certificate 子资源推进和查询唯一流程；所有错误返回稳定错误码与 current_revision。服务支持 -addr=127.0.0.1:<port>，也支持以 PORT 端口号绑定 127.0.0.1:<PORT>，默认监听 127.0.0.1:19081，禁止默认绑定 8080、80、3000 或 0.0.0.0。

## 标准构建、运行和测试命令
进入容器后执行：
cd '/app' && GOTOOLCHAIN=local go build ./...

cd /app && GOTOOLCHAIN=local go run ./cmd/paperqual -self-check -addr=127.0.0.1:19081

cd '/app' && GOTOOLCHAIN=local go test ./...

## Docker 构建和进入容器
chmod +x build_benzhi_docker.sh

./build_benzhi_docker.sh benzhi-project-1fe7f2b0-df79-4960-93a6-5fa93d4b0f98-amd64 linux/amd64

./build_benzhi_docker.sh benzhi-project-1fe7f2b0-df79-4960-93a6-5fa93d4b0f98-arm64 linux/arm64

docker run -it benzhi-project-1fe7f2b0-df79-4960-93a6-5fa93d4b0f98-amd64:latest

## 题目验证命令
1. 预期退出码 0：`go test ./...`
2. 预期退出码 0：`go run ./cmd/paperqual -self-check -addr=127.0.0.1:19081`
