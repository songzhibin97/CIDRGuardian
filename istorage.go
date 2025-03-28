package CIDRGuardian

import "context"

// IPStorage 是 IP 池存储的接口
type IPStorage interface {
	// AddIP 添加一个 IP 到可用池
	AddIP(ctx context.Context, ip string) error

	// RemoveIP 从可用池中移除一个 IP
	RemoveIP(ctx context.Context, ip string) error

	// IsIPAvailable 检查 IP 是否可用
	IsIPAvailable(ctx context.Context, ip string) (bool, error)

	// GetAvailableIPs 获取所有可用的 IP
	GetAvailableIPs(ctx context.Context) ([]string, error)

	// AllocateIP 分配一个 IP
	AllocateIP(ctx context.Context, ip string, description string) error

	// DeallocateIP 释放一个已分配的 IP
	DeallocateIP(ctx context.Context, ip string) error

	// GetAllocatedIPs 获取所有已分配的 IP 及描述
	GetAllocatedIPs(ctx context.Context) (map[string]string, error)

	// AvailableCount 获取可用 IP 数量
	AvailableCount(ctx context.Context) (int, error)

	// AllocatedCount 获取已分配 IP 数量
	AllocatedCount(ctx context.Context) (int, error)
}
