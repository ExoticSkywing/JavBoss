# JavDB 输入中心边界

第一阶段只负责：

1. 接收单个或批量番号；
2. 通过 JavDB App API 严格匹配作品；
3. 获取该作品的候选磁链；
4. 展示 `size`、`hd`、`cnsub`、文件数量、文件名和哈希；
5. 在前端筛选、排序并人工确认，复制标准磁链。

这一阶段不提交下载、不访问 115/CD2、不创建 JavBoss 数据库记录，也不触发目录扫描。

## 与 MDCx 上游的关系

`/home/lenovo/data/repo/mdcx-diy` 保留两个 Git remote：

- `origin`：`ExoticSkywing/mdcx-diy`（本地 Fork）；
- `upstream`：`cdlongbow/mdcx-diy`（官方源）。

JavBoss 不依赖完整 MDCx 镜像运行。MDCx 上游的 JavDB 适配逻辑更新时，先在 `mdcx-diy` 中
`fetch upstream` 并通过测试，再将必要的协议变化移植到 `internal/javdbinput`。不直接自动合并
到生产服务，避免上游接口变化影响原生 JavBoss。

磁链协议同时对照 [FlanChanXwO/javdb-cli](https://github.com/FlanChanXwO/javdb-cli) 的
App API 实现；其中已确认 `size` 为 MiB，并提供同一磁链接口、筛选与质量排序语义。后续更新时
以真实接口测试为准，只移植必要的协议变化，不增加新的运行时依赖。

## 接口

受保护的 `POST /jav/input/resolve` 接收：

```json
{"numbers":["DPMX-004","SSIS-589"]}
```

响应中的 `items` 与输入番号一一对应（去重后），单个番号失败只填充该项的 `error`，不影响同批次
其它结果。磁链 URI 由 `hash` 构造；JavDB 的 `size` 单位是 MiB，界面按 MB/GB 显示，并支持
`500MB`、`5GB` 形式的体积筛选。
