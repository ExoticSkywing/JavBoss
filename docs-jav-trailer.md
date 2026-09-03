# JAV 预告片轻量链路

JavMoe 的预告片能力不依赖运行中的 MDCx 服务：

1. 作品详情通过受保护的 `GET /jav/items/:id/trailer` 请求预告；
2. 主线路查询 TheJavDB API 中已整理的 DMM `sample_movie_url`，只接受番号精确匹配和 DMM HTTPS 媒体域名；
3. 主线路无预告时，回退到 JavDB App 的 `/api/v2/search` 与 `/api/v4/movies/{id}`；
4. 成功结果在 JavMoe 内存缓存 6 小时，确认无结果缓存 30 分钟；
5. 前端只在作品详情操作栏增加“播放预告”，使用现有 Video.js 播放能力，不覆盖任何原生元数据。

## 与 MDCx 上游的关系

当前轻量实现吸收并跟踪 MDCx 的以下适配器：

- `mdcx/crawlers/thejavdb_api.py`：DMM 托管预告 URL 主线路；
- `mdcx/crawlers/javdb_app.py`：JavDB App 详情预告回退线路；
- `mdcx/crawlers/dmm/__init__.py`：DMM URL 规范、质量与可用性规则的参考实现。

更新 MDCx 上游后，只把必要的协议或字段变化移植到 `internal/jav/trailer.go` 和
`internal/jav/javdb_app.go`，不把 Python、PyQt、OpenCV 或完整 MDCx 镜像加入 JavMoe 运行时。

真实来源回归可执行：

```bash
JAVBOSS_LIVE_TEST=1 go test ./internal/jav -run TestTrailerResolverLiveDPMX -count=1 -v
```
