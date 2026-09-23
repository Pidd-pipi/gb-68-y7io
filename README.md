# 智能灌溉管理系统

面向庭院花园和社区绿化场景的智能灌溉管理系统后端 API 服务，通过传感器数据采集和智能算法实现自动化灌溉调度。

## 快速启动

### Docker Compose 一键部署（推荐）

```bash
docker compose up -d
```

服务启动后：
- 后端 API: http://localhost:3109
- API 文档: http://localhost:3109/swagger/index.html
- PostgreSQL: localhost:5609
- Redis: localhost:6509

### 本地开发

1. 启动依赖服务：
```bash
docker compose up -d postgres redis
```

2. 进入 backend 目录：
```bash
cd backend
```

3. 复制环境变量：
```bash
cp .env.example .env
```

4. 运行服务：
```bash
go run main.go
```

## 项目主要功能

| 功能模块 | 说明 |
|---------|------|
| 设备管理 | 灌溉设备（电磁阀、水泵、传感器）CRUD，设备分组，在线检测，心跳上报 |
| 数据采集 | 土壤湿度、环境温度、降雨量等传感器数据采集，时序存储 |
| 灌溉计划 | 定时灌溉、条件灌溉，计划增删改查，启用/禁用 |
| 区域停灌 | 区域检修/暴雨时临时停灌，定时、条件、手动灌溉自动跳过并记录，紧急情况可书面登记后立即启动 |
| 智能调度 | 后台定时任务，结合传感器数据和天气预报自动决策 |
| 执行记录 | 灌溉执行日志，触发方式，用水量估算，历史统计 |
| 用水统计 | 日/周/月统计，区域占比分析，历史对比，节水建议 |
| 告警通知 | 设备离线、传感器异常、灌溉失败告警 |
| 系统配置 | 灌溉区域、设备参数、用户偏好设置 |

## 区域停灌

花园管理员遇到区域检修或暴雨时，可为区域设置临时停灌（需填写原因和预计结束时间）：

- 停灌期间，该区域的**定时计划、条件触发和普通手动浇水**都会被自动跳过，并留下跳过记录
- 同一区域重复设置只更新同一条停灌状态，不会产生多条记录
- 到达预计结束时间后停灌自动解除，也可提前手动解除；解除后历史记录仍可查询
- 遇到已断水、设备漏水等紧急情况，管理员填写书面原因后可**紧急启动**灌溉，不受停灌限制，原停灌状态仍按预计时间解除
- 区域详情接口返回当前停灌状态、原因和最近跳过记录

主要接口：

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/zones/{id}/suspension` | 设置停灌（body: `reason`, `expected_end_time`），重复设置更新同一条 |
| GET | `/api/zones/{id}/suspension` | 查询当前停灌状态 |
| POST | `/api/zones/{id}/suspension/resume` | 手动解除停灌 |
| GET | `/api/zones/{id}/suspension/history` | 停灌历史（含已解除） |
| GET | `/api/zones/{id}/suspension/skips` | 停灌期间的跳过记录 |
| POST | `/api/irrigation/emergency` | 紧急启动（body: `zone_id`, `reason` 书面原因必填） |

## 技术栈

| 技术 | 说明 |
|------|------|
| Go 1.21 | 编程语言 |
| Gin | Web 框架 |
| GORM | ORM 框架 |
| PostgreSQL | 主数据库 |
| Redis | 缓存/设备状态/任务队列 |
| JWT | 身份认证 |
| Swagger (Swaggo) | API 文档 |
| Zap | 结构化日志 |
| Viper | 配置管理 |
| Docker Compose | 容器编排 |

## 项目目录结构

```
gb-68-1/
├── backend/                    # 后端项目根目录
│   ├── cmd/                    # 应用入口
│   │   └── main.go
│   ├── internal/               # 内部代码
│   │   ├── config/            # 配置加载
│   │   ├── models/            # 数据模型
│   │   ├── controllers/       # HTTP 控制器
│   │   ├── services/          # 业务逻辑
│   │   ├── repositories/      # 数据访问层
│   │   ├── middleware/        # 中间件
│   │   ├── routes/            # 路由注册
│   │   ├── scheduler/         # 定时任务调度
│   │   └── utils/             # 工具函数
│   ├── pkg/                   # 公共包
│   │   ├── logger/            # 日志封装
│   │   ├── database/          # 数据库连接
│   │   ├── redis/             # Redis 连接
│   │   └── response/          # 响应封装
│   ├── Dockerfile             # 后端 Docker 镜像
│   ├── go.mod
│   └── .env.example
├── database/                  # 数据库脚本
│   └── init.sql
├── docs/                      # Swagger 文档（自动生成）
├── docker-compose.yml         # Docker 编排
├── .env.example               # 环境变量示例
└── README.md
```

## 环境变量说明

| 变量名 | 默认值 | 说明 |
|--------|--------|------|
| APP_ENV | development | 运行环境 (development/production) |
| APP_PORT | 8080 | 应用端口 |
| POSTGRES_HOST | localhost | PostgreSQL 主机 |
| POSTGRES_PORT | 5432 | PostgreSQL 端口 |
| POSTGRES_DB | irrigation | 数据库名 |
| POSTGRES_USER | postgres | 数据库用户 |
| POSTGRES_PASSWORD | postgres123 | 数据库密码 |
| REDIS_HOST | localhost | Redis 主机 |
| REDIS_PORT | 6379 | Redis 端口 |
| REDIS_PASSWORD | (空) | Redis 密码 |
| JWT_SECRET | your-secret-key | JWT 密钥 |
| JWT_EXPIRE_HOURS | 24 | JWT 过期时间（小时） |
| LOG_LEVEL | info | 日志级别 (debug/info/warn/error) |

## Docker 部署说明

### 端口映射

| 服务 | 容器端口 | 主机端口 |
|------|----------|----------|
| 后端 API | 8080 | 3109 |
| PostgreSQL | 5432 | 5609 |
| Redis | 6379 | 6509 |

### 数据卷

- `postgres-data`: PostgreSQL 数据持久化
- `redis-data`: Redis 数据持久化

### 常见问题

1. **端口冲突**
   ```bash
   # 查看端口占用
   lsof -i :3109
   lsof -i :5609
   lsof -i :6509
   ```

2. **服务启动失败**
   ```bash
   # 查看日志
   docker compose logs backend
   docker compose logs postgres
   ```

3. **重新构建**
   ```bash
   docker compose up -d --build
   ```

4. **清理所有数据**
   ```bash
   docker compose down -v
   ```

## License

MIT
