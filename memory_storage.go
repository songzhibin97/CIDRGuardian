package CIDRGuardian

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// MemoryIPStorage 是 IP 池存储的内存实现
type MemoryIPStorage struct {
	mu        sync.RWMutex
	available map[string]bool
	allocated map[string]string
}

// NewMemoryIPStorage 创建一个新的内存 IP 存储
func NewMemoryIPStorage() *MemoryIPStorage {
	return &MemoryIPStorage{
		available: make(map[string]bool),
		allocated: make(map[string]string),
	}
}

// AddIP 实现 IPStorage 接口
func (s *MemoryIPStorage) AddIP(ctx context.Context, ip string) error {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// 如果 IP 已被分配，不能添加到可用池
	if _, exists := s.allocated[ip]; exists {
		return fmt.Errorf("IP %s 已被分配", ip)
	}

	s.available[ip] = true
	return nil
}

// RemoveIP 实现 IPStorage 接口
func (s *MemoryIPStorage) RemoveIP(ctx context.Context, ip string) error {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.available[ip]; !exists {
		return fmt.Errorf("IP %s 不在可用池中", ip)
	}

	delete(s.available, ip)
	return nil
}

// IsIPAvailable 实现 IPStorage 接口
func (s *MemoryIPStorage) IsIPAvailable(ctx context.Context, ip string) (bool, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return false, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	_, exists := s.available[ip]
	return exists, nil
}

// GetAvailableIPs 实现 IPStorage 接口
func (s *MemoryIPStorage) GetAvailableIPs(ctx context.Context) ([]string, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	ips := make([]string, 0, len(s.available))
	for ip := range s.available {
		ips = append(ips, ip)
	}

	sort.Strings(ips)
	return ips, nil
}

// AllocateIP 实现 IPStorage 接口
func (s *MemoryIPStorage) AllocateIP(ctx context.Context, ip string, description string) error {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.available[ip]; !exists {
		return fmt.Errorf("IP %s 不在可用池中", ip)
	}

	delete(s.available, ip)
	s.allocated[ip] = description
	return nil
}

// DeallocateIP 实现 IPStorage 接口
func (s *MemoryIPStorage) DeallocateIP(ctx context.Context, ip string) error {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.allocated[ip]; !exists {
		return fmt.Errorf("IP %s 不在已分配池中", ip)
	}

	delete(s.allocated, ip)
	s.available[ip] = true
	return nil
}

// GetAllocatedIPs 实现 IPStorage 接口
func (s *MemoryIPStorage) GetAllocatedIPs(ctx context.Context) (map[string]string, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]string)
	for ip, desc := range s.allocated {
		result[ip] = desc
	}

	return result, nil
}

// AvailableCount 实现 IPStorage 接口
func (s *MemoryIPStorage) AvailableCount(ctx context.Context) (int, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.available), nil
}

// AllocatedCount 实现 IPStorage 接口
func (s *MemoryIPStorage) AllocatedCount(ctx context.Context) (int, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.allocated), nil
}
