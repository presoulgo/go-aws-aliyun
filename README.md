# 云枢 · AWS + 阿里云运维聚合平台

一个可接入真实 AWS 与阿里云账号的只读多云资源与监控工作台。Go 服务将 Vue 页面嵌入二进制，使用 SQLite WAL 保存资源、用户和同步历史；演示模式仅用于没有云凭证时试用界面。

## 快速开始

需要 Go 1.26、Node.js 22 和 npm。`go.mod` 会通过 `GOTOOLCHAIN` 自动安装匹配工具链。

```sh
make build
OPS_SECRET_KEY='replace-with-a-long-random-secret' \
OPS_DB_PATH=data/ops.db \
./bin/go-aws-aliyun
```

Windows PowerShell：

```powershell
cd web
npm ci
npm run build
cd ..
go build -o bin/go-aws-aliyun.exe ./cmd/server
$env:OPS_SECRET_KEY='replace-with-a-long-random-secret'
$env:OPS_DB_PATH='data/ops.db'
.\bin\go-aws-aliyun.exe
```

打开 `http://localhost:8080`。首次启动时，服务端日志打印 `admin` 的随机初始密码；也可提前设置 `OPS_ADMIN_PASSWORD`。登录后在“云账号”中填写只读 AccessKey，可选填 AWS IAM Role ARN 或阿里云 RAM Role ARN。测试连接会从云端读取账号 UID 和可用地域；保存后立即启动第一次真实资源同步。

生产部署必须设置持久且保密的 `OPS_SECRET_KEY`，用于加密云账号 Secret 和签发登录令牌。建议使用至少 32 个随机字符，保存在外部密钥管理系统或运行环境变量中。不要把它写入配置文件或提交到仓库。

没有可用云凭证时，可设置 `OPS_DEMO=true OPS_DB_PATH=data/demo.db` 启动演示模式。它会生成 6 个云账号、27 个账号地域和 784 项资源；演示数据只在数据库没有云账号时填充。

可把 `configs/config.example.yaml` 复制为 `configs/config.yaml`，或设置 `OPS_CONFIG` 指向配置文件。环境变量优先于 YAML；支持 `OPS_ADDR`、`OPS_DB_PATH`、`OPS_APP_NAME`、`OPS_IDLE_CPU_THRESHOLD`、`OPS_EXPIRING_DAYS`、`OPS_AUDIT_RETENTION_DAYS`。

## 页面与权限

管理员可新增和测试云账号、触发或取消同步、管理用户及查看审计日志。只读用户可查看概览、资源和监控；写接口在服务端返回 403。账号 Secret 使用 AES-GCM 加密存储，接口不返回 Secret。

资源同步按账号地域并发执行。AWS 分别采集 EC2、RDS、ELBv2、S3；阿里云分别采集 ECS、RDS、CLB、ALB、OSS。除基础清单外，同步会读取资源标签、网络属性、数据库容量与连接信息，以及对象存储容量和对象数。单个云 API 任务最长执行 3 分钟。每个成功任务会更新该范围资源，并删除该范围中已消失的资源；失败、跳过或取消的范围保留旧资源。主机 CPU 指标以限流并发方式采集。服务每 30 分钟自动同步启用账号，审计日志保留 180 天，CPU 小时数据保留 8 天。监控按需从 CloudWatch 或 CloudMonitor 查询，成功结果在内存缓存 2 分钟。

## 只读云权限

AWS 使用 IAM 专用用户的 AccessKey，或再配置只读 AssumeRole ARN。可使用托管策略 `ReadOnlyAccess`；最小动作至少包含：

- `sts:GetCallerIdentity`
- `ec2:DescribeRegions`、`ec2:DescribeInstances`、`ec2:DescribeInstanceTypes`
- `rds:DescribeDBInstances`、`rds:ListTagsForResource`
- `elasticloadbalancing:DescribeLoadBalancers`、`elasticloadbalancing:DescribeTags`
- `s3:ListAllMyBuckets`、`s3:GetBucketLocation`、`s3:GetBucketTagging`
- `cloudwatch:GetMetricStatistics`、`cloudwatch:GetMetricData`

AWS 中国区使用 `cn-north-1` 或 `cn-northwest-1` 的凭证与分区。阿里云使用 RAM 专用用户，授予 `AliyunECSReadOnlyAccess`、`AliyunRDSReadOnlyAccess`、`AliyunSLBReadOnlyAccess`、`AliyunALBReadOnlyAccess`、`AliyunOSSReadOnlyAccess` 和 `AliyunCloudMonitorReadOnlyAccess`，并允许 STS `GetCallerIdentity`。所有云操作只调用查询类 API。

## 开发与扩展

后端入口在 `cmd/server`，数据模型在 `internal/model`，云厂商实现 `internal/cloud.Provider`，同步与 HTTP 接口在 `internal/api`。前端页面在 `web/src/views`，视觉变量在 `web/src/styles/theme.css`。增加资源类型时，在 Provider 的 `List` 中映射统一 `Resource`，再增加同步任务与资源中心 Tab。

设计依据见 [原型画布](https://claude.ai/artifact/BTSS55qUzW1qQfcBnkLuXu) 和 `design/prototype/README.md`。

没有真实云凭证的开发环境只能验证演示模式；上线前应使用组织自己的只读测试账号确认 API 授权、地域和指标可用性。
