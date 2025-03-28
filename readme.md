# CIDRGuardian

CIDRGuardian 是一个强大的 Go 语言 IP 地址和 CIDR 管理库，用于高效地管理、分配和追踪 IP 资源。

## 功能特点

- 完整的 IP/CIDR 管理生命周期
- 支持多种存储后端 (内存、MySQL、PostgreSQL)
- 高度并发安全
- 支持 IPv4 (未来将支持 IPv6)
- 网络对齐的 CIDR 分配
- 详细的资源使用追踪
- 完善的测试覆盖

## 安装

```bash
go get github.com/songzhibin97/CIDRGuardian
```

## 基本用法

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/songzhibin97/CIDRGuardian"
)

func main() {
    ctx := context.Background()
    
    // 创建一个内存存储的 CIDRGuardian
    guardian, err := CIDRGuardian.NewCIDRGuardian(ctx, nil, "192.168.0.0/24")
    if err != nil {
        log.Fatalf("创建 CIDRGuardian 失败: %v", err)
    }
    
    // 分配一个 IP
    ip, err := guardian.GetNextAvailableIP(ctx, "Web 服务器")
    if err != nil {
        log.Fatalf("分配 IP 失败: %v", err)
    }
    fmt.Printf("分配了 IP: %s\n", ip)
    
    // 分配一个 CIDR 子网
    cidr, err := guardian.AllocateCIDR(ctx, 28, "数据库集群")
    if err != nil {
        log.Fatalf("分配 CIDR 失败: %v", err)
    }
    fmt.Printf("分配了 CIDR: %s\n", cidr)
    
    // 查看当前状态
    status, _ := guardian.String(ctx)
    fmt.Println(status)
}
```

## 使用 SQL 存储后端

### MySQL

```go
import (
    "context"
    "log"
    "time"

    "github.com/songzhibin97/CIDRGuardian"
)

func main() {
    ctx := context.Background()
    
    // 创建 MySQL 配置
    config := CIDRGuardian.SQLConfig{
        DriverName:      "mysql",
        DataSourceName:  "user:password@tcp(127.0.0.1:3306)/cidrguardian?parseTime=true",
        MaxOpenConns:    10,
        MaxIdleConns:    5,
        ConnMaxLifetime: time.Hour,
    }
    
    // 创建 SQL 存储
    storage, err := CIDRGuardian.NewSQLIPStorage(ctx, config)
    if err != nil {
        log.Fatalf("创建 SQL 存储失败: %v", err)
    }
    defer storage.Close()
    
    // 创建使用 SQL 存储的 CIDRGuardian
    guardian, err := CIDRGuardian.NewCIDRGuardian(ctx, storage, "192.168.0.0/24")
    if err != nil {
        log.Fatalf("创建 CIDRGuardian 失败: %v", err)
    }
    
    // 现在可以使用 guardian 进行各种操作...
}
```

### PostgreSQL

```go
import (
    "context"
    "log"
    "time"

    "github.com/songzhibin97/CIDRGuardian"
)

func main() {
    ctx := context.Background()
    
    // 创建 PostgreSQL 配置
    config := CIDRGuardian.SQLConfig{
        DriverName:      "postgres",
        DataSourceName:  "postgres://user:password@localhost/cidrguardian?sslmode=disable",
        MaxOpenConns:    10,
        MaxIdleConns:    5,
        ConnMaxLifetime: time.Hour,
    }
    
    // 创建 SQL 存储
    storage, err := CIDRGuardian.NewSQLIPStorage(ctx, config)
    if err != nil {
        log.Fatalf("创建 SQL 存储失败: %v", err)
    }
    defer storage.Close()
    
    // 创建使用 SQL 存储的 CIDRGuardian
    guardian, err := CIDRGuardian.NewCIDRGuardian(ctx, storage, "10.0.0.0/16")
    if err != nil {
        log.Fatalf("创建 CIDRGuardian 失败: %v", err)
    }
    
    // 现在可以使用 guardian 进行各种操作...
}
```

## 主要 API

### CIDRGuardian

- `NewCIDRGuardian(ctx, storage, initialCIDRs...)` - 创建一个新的 CIDRGuardian
- `AddCIDR(ctx, cidr, description)` - 添加一个 CIDR 到管理池
- `RemoveCIDR(ctx, cidr)` - 从管理池中移除一个 CIDR
- `GetManagedCIDRs(ctx)` - 获取所有管理的 CIDR
- `AllocateIP(ctx, ip, description)` - 分配一个特定的 IP
- `GetNextAvailableIP(ctx, description)` - 获取下一个可用的 IP
- `AllocateCIDR(ctx, bits, description)` - 分配一个特定大小的 CIDR
- `ReleaseIP(ctx, ip)` - 释放一个分配的 IP
- `ReleaseCIDR(ctx, cidr)` - 释放一个分配的 CIDR
- `GetAvailableCIDRs(ctx)` - 获取可用的 CIDR
- `GetUsedCIDRs(ctx)` - 获取已使用的 CIDR
- `AvailableCount(ctx)` - 获取可用 IP 数量
- `AllocatedCount(ctx)` - 获取已分配 IP 数量
- `String(ctx)` - 获取人类可读的状态报告

### IPStorage 接口

CIDRGuardian 支持可插拔的存储后端。任何实现了 `IPStorage` 接口的类型都可以用作存储：

```go
type IPStorage interface {
    AddIP(ctx context.Context, ip string) error
    RemoveIP(ctx context.Context, ip string) error
    IsIPAvailable(ctx context.Context, ip string) (bool, error)
    GetAvailableIPs(ctx context.Context) ([]string, error)
    AllocateIP(ctx context.Context, ip string, description string) error
    DeallocateIP(ctx context.Context, ip string) error
    GetAllocatedIPs(ctx context.Context) (map[string]string, error)
    AvailableCount(ctx context.Context) (int, error)
    AllocatedCount(ctx context.Context) (int, error)
}
```

CIDRGuardian 提供了两种内置实现：
- `MemoryIPStorage` - 内存存储，适合单实例应用
- `SQLIPStorage` - SQL 存储，支持 MySQL 和 PostgreSQL，适合多实例应用和需要持久化的场景

## 高级用例

### 扩展 IP 池

```go
// 添加新的 CIDR 到管理池
err := guardian.AddCIDR(ctx, "172.16.0.0/24", "新的部门网络")
if err != nil {
    log.Fatalf("添加 CIDR 失败: %v", err)
}
```

### CIDR 分配

```go
// 分配 /29 子网 (8 个 IP 地址)
cidr, err := guardian.AllocateCIDR(ctx, 29, "IOT 设备网络")
if err != nil {
    log.Fatalf("分配 CIDR 失败: %v", err)
}
fmt.Printf("分配了 CIDR: %s\n", cidr)

// 释放 CIDR
err = guardian.ReleaseCIDR(ctx, cidr)
if err != nil {
    log.Fatalf("释放 CIDR 失败: %v", err)
}
```

### 获取使用情况统计

```go
availableCount, _ := guardian.AvailableCount(ctx)
allocatedCount, _ := guardian.AllocatedCount(ctx)

fmt.Printf("可用 IP: %d\n", availableCount)
fmt.Printf("已分配 IP: %d\n", allocatedCount)
fmt.Printf("总利用率: %.2f%%\n", float64(allocatedCount)/float64(availableCount+allocatedCount)*100)
```

## 实现自定义存储后端

你可以通过实现 `IPStorage` 接口来创建自定义存储后端：

```go
type MyCustomStorage struct {
    // 你的字段
}

// 实现所有 IPStorage 接口方法...

// 然后使用你的自定义存储
guardian, err := CIDRGuardian.NewCIDRGuardian(ctx, &MyCustomStorage{}, "192.168.0.0/24")
```

## 许可证

MIT

## 贡献

欢迎贡献！请随时提交 Pull Request 或创建 Issue。