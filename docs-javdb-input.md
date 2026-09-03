# JavDB 磁链来源边界

> 作品集合、番号输入、库存状态和完整入库状态机的权威定义见
> [docs-jav-lifecycle.md](docs-jav-lifecycle.md)。本文只描述 JavDB 作为元数据与磁链候选来源的协议边界。

JavDB 适配层负责：

1. 通过 JavDB App API 严格匹配作品；
2. 获取该作品的候选磁链；
3. 展示 `size`、`hd`、`cnsub`、文件数量、文件名和哈希；
4. 返回来源能确认的客观字段，供后续持久化、筛选与人工标注。

JavDB 查询不提交下载、不访问 115/CD2，也不替代目录扫描。番号输入会先创建或命中正式 `Jav`；
候选磁链后续必须绑定该 `jav_id`，不能绑定输入批次。详情页保存选择与发送下载是两个动作，单个
或批量发送都进入同一批次模型。外部服务通过 `JAVBOSS_CLOUD_DOWNLOAD_URL` 接收发送请求，并以
幂等键去重；未配置地址时不创建发送批次，已保存选择继续留在待发送队列。文件落盘后还必须经过质量验收，
“已看到文件”和“正式入库”不是同一个事实。

## 与 MDCx 上游的关系

`/home/lenovo/data/repo/mdcx-diy` 保留两个 Git remote：

- `origin`：`ExoticSkywing/mdcx-diy`（本地 Fork）；
- `upstream`：`cdlongbow/mdcx-diy`（官方源）。

JavMoe 不依赖完整 MDCx 镜像运行。MDCx 上游的 JavDB 适配逻辑更新时，先在 `mdcx-diy` 中
`fetch upstream` 并通过测试，再将必要的协议变化移植到 `internal/jav/javdb_app.go`。不直接自动合并
到生产服务，避免上游接口变化影响原生 JavMoe。

磁链协议同时对照 [FlanChanXwO/javdb-cli](https://github.com/FlanChanXwO/javdb-cli) 的
App API 实现；其中已确认 `size` 为 MiB，并提供同一磁链接口、筛选与质量排序语义。后续更新时
以真实接口测试为准，只移植必要的协议变化，不增加新的运行时依赖。

预告片的两级来源与上游同步边界见 `docs-jav-trailer.md`。

## 接口

受保护的 `POST /jav/input/resolve` 接收：

```json
{"numbers":["DPMX-004","SSIS-589"]}
```

响应中的 `items` 与输入番号一一对应（去重后），单个番号失败只填充该项的 `error`，不影响同批次
其它结果。磁链 URI 由 `hash` 构造；JavDB 的 `size` 单位是 MiB，界面按 MB/GB 显示，并支持
`500MB`、`5GB` 形式的体积筛选。

受 3–8 秒限流约束的 JavDB 网络查询不能阻塞“加入作品库”的本地事务。元数据和候选获取应在
作品创建后异步执行；单项远端失败不能回滚已经成立的全局作品身份。服务启动后由独立的后台磁链
扫描器处理 `magnet_collecting` 阶段，元数据补全后会主动唤醒扫描器；详情页的按钮只是立即重试入口，
不再是获取磁链的唯一方式。

作品详情页可通过 `POST /jav/items/:id/magnets/collect` 重新向 JavDB 收集磁链。候选以
`(jav_id, info_hash)` 幂等保存；重新采集只刷新名称、大小、HD/中字、文件数和来源时间等自动事实，
不得覆盖人工验收字段，也不得删除历史不合格候选。

样品图像不要求浏览器直接访问第三方 CDN。详情页使用
`GET /jav/items/:id/sample-images/:index` 由 JavMoe 同源代理已保存的图片，服务端校验来源域名、
限制响应大小并带上必要的请求头，适配远程开发和 VS Code 端口转发环境。
