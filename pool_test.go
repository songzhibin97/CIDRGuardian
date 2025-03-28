package CIDRGuardian

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

// mockIPStorage 是用于测试的存储模拟实现
type mockIPStorage struct {
	available map[string]bool
	allocated map[string]string
	failOn    string // 用于触发特定错误的操作名
	errorMsg  string // 错误消息
}

func newMockIPStorage() *mockIPStorage {
	return &mockIPStorage{
		available: make(map[string]bool),
		allocated: make(map[string]string),
	}
}

// 设置mock失败的操作
func (m *mockIPStorage) setFailure(operation, errorMsg string) {
	m.failOn = operation
	m.errorMsg = errorMsg
}

// AddIP 实现 IPStorage 接口
func (m *mockIPStorage) AddIP(ctx context.Context, ip string) error {
	if m.failOn == "AddIP" {
		return errors.New(m.errorMsg)
	}

	if _, exists := m.allocated[ip]; exists {
		return errors.New("IP already allocated")
	}
	m.available[ip] = true
	return nil
}

// RemoveIP 实现 IPStorage 接口
func (m *mockIPStorage) RemoveIP(ctx context.Context, ip string) error {
	if m.failOn == "RemoveIP" {
		return errors.New(m.errorMsg)
	}

	if _, exists := m.available[ip]; !exists {
		return errors.New("IP not available")
	}
	delete(m.available, ip)
	return nil
}

// IsIPAvailable 实现 IPStorage 接口
func (m *mockIPStorage) IsIPAvailable(ctx context.Context, ip string) (bool, error) {
	if m.failOn == "IsIPAvailable" {
		return false, errors.New(m.errorMsg)
	}

	_, exists := m.available[ip]
	return exists, nil
}

// GetAvailableIPs 实现 IPStorage 接口
func (m *mockIPStorage) GetAvailableIPs(ctx context.Context) ([]string, error) {
	if m.failOn == "GetAvailableIPs" {
		return nil, errors.New(m.errorMsg)
	}

	ips := make([]string, 0, len(m.available))
	for ip := range m.available {
		ips = append(ips, ip)
	}
	sort.Strings(ips)
	return ips, nil
}

// AllocateIP 实现 IPStorage 接口
func (m *mockIPStorage) AllocateIP(ctx context.Context, ip string, description string) error {
	if m.failOn == "AllocateIP" {
		return errors.New(m.errorMsg)
	}

	if _, exists := m.available[ip]; !exists {
		return errors.New("IP not available")
	}
	delete(m.available, ip)
	m.allocated[ip] = description
	return nil
}

// DeallocateIP 实现 IPStorage 接口
func (m *mockIPStorage) DeallocateIP(ctx context.Context, ip string) error {
	if m.failOn == "DeallocateIP" {
		return errors.New(m.errorMsg)
	}

	if _, exists := m.allocated[ip]; !exists {
		return errors.New("IP not allocated")
	}
	delete(m.allocated, ip)
	m.available[ip] = true
	return nil
}

// GetAllocatedIPs 实现 IPStorage 接口
func (m *mockIPStorage) GetAllocatedIPs(ctx context.Context) (map[string]string, error) {
	if m.failOn == "GetAllocatedIPs" {
		return nil, errors.New(m.errorMsg)
	}

	result := make(map[string]string)
	for ip, desc := range m.allocated {
		result[ip] = desc
	}
	return result, nil
}

// AvailableCount 实现 IPStorage 接口
func (m *mockIPStorage) AvailableCount(ctx context.Context) (int, error) {
	if m.failOn == "AvailableCount" {
		return 0, errors.New(m.errorMsg)
	}
	return len(m.available), nil
}

// AllocatedCount 实现 IPStorage 接口
func (m *mockIPStorage) AllocatedCount(ctx context.Context) (int, error) {
	if m.failOn == "AllocatedCount" {
		return 0, errors.New(m.errorMsg)
	}
	return len(m.allocated), nil
}

// TestNewMemoryIPStorage 测试内存存储的创建
func TestNewMemoryIPStorage(t *testing.T) {
	storage := NewMemoryIPStorage()
	if storage == nil {
		t.Fatal("Expected non-nil storage")
	}
	if storage.available == nil {
		t.Error("Available map should be initialized")
	}
	if storage.allocated == nil {
		t.Error("Allocated map should be initialized")
	}
}

// TestMemoryIPStorage_AddIP 测试添加IP
func TestMemoryIPStorage_AddIP(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryIPStorage()

	// 测试正常添加
	err := storage.AddIP(ctx, "192.168.1.1")
	if err != nil {
		t.Errorf("AddIP should succeed: %v", err)
	}

	// 测试重复添加
	storage.allocated["192.168.1.1"] = "test"
	err = storage.AddIP(ctx, "192.168.1.1")
	if err == nil {
		t.Error("AddIP should fail when IP is already allocated")
	}

	// 测试上下文取消
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	err = storage.AddIP(canceledCtx, "192.168.1.2")
	if err == nil {
		t.Error("AddIP should fail when context is canceled")
	}
}

// TestMemoryIPStorage_RemoveIP 测试移除IP
func TestMemoryIPStorage_RemoveIP(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryIPStorage()

	// 添加一个IP
	storage.available["192.168.1.1"] = true

	// 测试正常移除
	err := storage.RemoveIP(ctx, "192.168.1.1")
	if err != nil {
		t.Errorf("RemoveIP should succeed: %v", err)
	}

	// 测试移除不存在的IP
	err = storage.RemoveIP(ctx, "192.168.1.1")
	if err == nil {
		t.Error("RemoveIP should fail when IP is not available")
	}

	// 测试上下文取消
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	err = storage.RemoveIP(canceledCtx, "192.168.1.2")
	if err == nil {
		t.Error("RemoveIP should fail when context is canceled")
	}
}

// TestMemoryIPStorage_IsIPAvailable 测试检查IP是否可用
func TestMemoryIPStorage_IsIPAvailable(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryIPStorage()

	// 添加一个IP
	storage.available["192.168.1.1"] = true

	// 测试存在的IP
	available, err := storage.IsIPAvailable(ctx, "192.168.1.1")
	if err != nil {
		t.Errorf("IsIPAvailable should succeed: %v", err)
	}
	if !available {
		t.Error("IP should be available")
	}

	// 测试不存在的IP
	available, err = storage.IsIPAvailable(ctx, "192.168.1.2")
	if err != nil {
		t.Errorf("IsIPAvailable should succeed: %v", err)
	}
	if available {
		t.Error("IP should not be available")
	}

	// 测试上下文取消
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = storage.IsIPAvailable(canceledCtx, "192.168.1.1")
	if err == nil {
		t.Error("IsIPAvailable should fail when context is canceled")
	}
}

// TestMemoryIPStorage_GetAvailableIPs 测试获取可用IP列表
func TestMemoryIPStorage_GetAvailableIPs(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryIPStorage()

	// 添加两个IP
	storage.available["192.168.1.2"] = true
	storage.available["192.168.1.1"] = true

	// 测试获取列表
	ips, err := storage.GetAvailableIPs(ctx)
	if err != nil {
		t.Errorf("GetAvailableIPs should succeed: %v", err)
	}
	expected := []string{"192.168.1.1", "192.168.1.2"}
	if !reflect.DeepEqual(ips, expected) {
		t.Errorf("Expected %v, got %v", expected, ips)
	}

	// 测试空列表
	storage.available = make(map[string]bool)
	ips, err = storage.GetAvailableIPs(ctx)
	if err != nil {
		t.Errorf("GetAvailableIPs should succeed: %v", err)
	}
	if len(ips) != 0 {
		t.Errorf("Expected empty list, got %v", ips)
	}

	// 测试上下文取消
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = storage.GetAvailableIPs(canceledCtx)
	if err == nil {
		t.Error("GetAvailableIPs should fail when context is canceled")
	}
}

// TestMemoryIPStorage_AllocateIP 测试分配IP
func TestMemoryIPStorage_AllocateIP(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryIPStorage()

	// 添加一个IP
	storage.available["192.168.1.1"] = true

	// 测试正常分配
	err := storage.AllocateIP(ctx, "192.168.1.1", "test")
	if err != nil {
		t.Errorf("AllocateIP should succeed: %v", err)
	}
	if _, exists := storage.available["192.168.1.1"]; exists {
		t.Error("IP should be removed from available")
	}
	if desc, exists := storage.allocated["192.168.1.1"]; !exists || desc != "test" {
		t.Error("IP should be added to allocated")
	}

	// 测试分配不存在的IP
	err = storage.AllocateIP(ctx, "192.168.1.2", "test")
	if err == nil {
		t.Error("AllocateIP should fail when IP is not available")
	}

	// 测试上下文取消
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	err = storage.AllocateIP(canceledCtx, "192.168.1.1", "test")
	if err == nil {
		t.Error("AllocateIP should fail when context is canceled")
	}
}

// TestMemoryIPStorage_DeallocateIP 测试释放IP
func TestMemoryIPStorage_DeallocateIP(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryIPStorage()

	// 添加一个已分配的IP
	storage.allocated["192.168.1.1"] = "test"

	// 测试正常释放
	err := storage.DeallocateIP(ctx, "192.168.1.1")
	if err != nil {
		t.Errorf("DeallocateIP should succeed: %v", err)
	}
	if _, exists := storage.allocated["192.168.1.1"]; exists {
		t.Error("IP should be removed from allocated")
	}
	if _, exists := storage.available["192.168.1.1"]; !exists {
		t.Error("IP should be added to available")
	}

	// 测试释放不存在的IP
	err = storage.DeallocateIP(ctx, "192.168.1.1")
	if err == nil {
		t.Error("DeallocateIP should fail when IP is not allocated")
	}

	// 测试上下文取消
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	err = storage.DeallocateIP(canceledCtx, "192.168.1.2")
	if err == nil {
		t.Error("DeallocateIP should fail when context is canceled")
	}
}

// TestMemoryIPStorage_GetAllocatedIPs 测试获取已分配IP列表
func TestMemoryIPStorage_GetAllocatedIPs(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryIPStorage()

	// 添加两个已分配的IP
	storage.allocated["192.168.1.1"] = "test1"
	storage.allocated["192.168.1.2"] = "test2"

	// 测试获取列表
	ips, err := storage.GetAllocatedIPs(ctx)
	if err != nil {
		t.Errorf("GetAllocatedIPs should succeed: %v", err)
	}
	expected := map[string]string{
		"192.168.1.1": "test1",
		"192.168.1.2": "test2",
	}
	if !reflect.DeepEqual(ips, expected) {
		t.Errorf("Expected %v, got %v", expected, ips)
	}

	// 测试空列表
	storage.allocated = make(map[string]string)
	ips, err = storage.GetAllocatedIPs(ctx)
	if err != nil {
		t.Errorf("GetAllocatedIPs should succeed: %v", err)
	}
	if len(ips) != 0 {
		t.Errorf("Expected empty map, got %v", ips)
	}

	// 测试上下文取消
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = storage.GetAllocatedIPs(canceledCtx)
	if err == nil {
		t.Error("GetAllocatedIPs should fail when context is canceled")
	}
}

// TestMemoryIPStorage_AvailableCount 测试获取可用IP数量
func TestMemoryIPStorage_AvailableCount(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryIPStorage()

	// 添加两个IP
	storage.available["192.168.1.1"] = true
	storage.available["192.168.1.2"] = true

	// 测试获取数量
	count, err := storage.AvailableCount(ctx)
	if err != nil {
		t.Errorf("AvailableCount should succeed: %v", err)
	}
	if count != 2 {
		t.Errorf("Expected 2, got %d", count)
	}

	// 测试空列表
	storage.available = make(map[string]bool)
	count, err = storage.AvailableCount(ctx)
	if err != nil {
		t.Errorf("AvailableCount should succeed: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected 0, got %d", count)
	}

	// 测试上下文取消
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = storage.AvailableCount(canceledCtx)
	if err == nil {
		t.Error("AvailableCount should fail when context is canceled")
	}
}

// TestMemoryIPStorage_AllocatedCount 测试获取已分配IP数量
func TestMemoryIPStorage_AllocatedCount(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryIPStorage()

	// 添加两个已分配的IP
	storage.allocated["192.168.1.1"] = "test1"
	storage.allocated["192.168.1.2"] = "test2"

	// 测试获取数量
	count, err := storage.AllocatedCount(ctx)
	if err != nil {
		t.Errorf("AllocatedCount should succeed: %v", err)
	}
	if count != 2 {
		t.Errorf("Expected 2, got %d", count)
	}

	// 测试空列表
	storage.allocated = make(map[string]string)
	count, err = storage.AllocatedCount(ctx)
	if err != nil {
		t.Errorf("AllocatedCount should succeed: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected 0, got %d", count)
	}

	// 测试上下文取消
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = storage.AllocatedCount(canceledCtx)
	if err == nil {
		t.Error("AllocatedCount should fail when context is canceled")
	}
}

// TestNewCIDRGuardian 测试创建CIDRGuardian
func TestNewCIDRGuardian(t *testing.T) {
	ctx := context.Background()

	// 测试正常创建，不带初始CIDR
	guardian, err := NewCIDRGuardian(ctx, nil)
	if err != nil {
		t.Fatalf("NewCIDRGuardian should succeed: %v", err)
	}
	if guardian == nil {
		t.Fatal("Expected non-nil guardian")
	}

	// 测试正常创建，带初始CIDR
	guardian, err = NewCIDRGuardian(ctx, nil, "192.168.0.0/24")
	if err != nil {
		t.Fatalf("NewCIDRGuardian should succeed: %v", err)
	}
	managed, err := guardian.GetManagedCIDRs(ctx)
	if err != nil {
		t.Fatalf("GetManagedCIDRs should succeed: %v", err)
	}
	if _, exists := managed["192.168.0.0/24"]; !exists {
		t.Error("Initial CIDR should be added")
	}

	// 测试创建失败，无效CIDR
	_, err = NewCIDRGuardian(ctx, nil, "invalid")
	if err == nil {
		t.Error("NewCIDRGuardian should fail with invalid CIDR")
	}

	// 测试上下文取消
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	// 这里使用一个 mock 存储而不是 nil，因为在上下文已取消的情况下，
	// 创建新的 MemoryIPStorage 可能在添加 CIDR 之前就已经返回
	mockStorage := newMockIPStorage()
	_, err = NewCIDRGuardian(canceledCtx, mockStorage)
	if err == nil || err != context.Canceled {
		t.Error("NewCIDRGuardian should fail with context.Canceled when context is canceled")
	}
}

// TestCIDRGuardian_AddCIDR 测试添加CIDR
func TestCIDRGuardian_AddCIDR(t *testing.T) {
	ctx := context.Background()
	mockStorage := newMockIPStorage()
	guardian, _ := NewCIDRGuardian(ctx, mockStorage)

	// 测试正常添加
	err := guardian.AddCIDR(ctx, "192.168.0.0/24", "test")
	if err != nil {
		t.Errorf("AddCIDR should succeed: %v", err)
	}
	managed, _ := guardian.GetManagedCIDRs(ctx)
	if _, exists := managed["192.168.0.0/24"]; !exists {
		t.Error("CIDR should be added")
	}

	// 测试添加无效CIDR
	err = guardian.AddCIDR(ctx, "invalid", "test")
	if err == nil {
		t.Error("AddCIDR should fail with invalid CIDR")
	}

	// 测试添加已存在的CIDR
	err = guardian.AddCIDR(ctx, "192.168.0.0/24", "test")
	if err == nil {
		t.Error("AddCIDR should fail when CIDR already exists")
	}

	// 测试上下文取消
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	err = guardian.AddCIDR(canceledCtx, "10.0.0.0/24", "test")
	if err == nil {
		t.Error("AddCIDR should fail when context is canceled")
	}

	// 测试存储失败
	mockStorage.setFailure("AddIP", "mock failure")
	err = guardian.AddCIDR(ctx, "172.16.0.0/24", "test")
	if err == nil {
		t.Error("AddCIDR should fail when storage fails")
	}
}

// TestCIDRGuardian_RemoveCIDR 测试移除CIDR
func TestCIDRGuardian_RemoveCIDR(t *testing.T) {
	ctx := context.Background()
	guardian, _ := NewCIDRGuardian(ctx, nil, "192.168.0.0/24")

	// 测试正常移除
	err := guardian.RemoveCIDR(ctx, "192.168.0.0/24")
	if err != nil {
		t.Errorf("RemoveCIDR should succeed: %v", err)
	}
	managed, _ := guardian.GetManagedCIDRs(ctx)
	if _, exists := managed["192.168.0.0/24"]; exists {
		t.Error("CIDR should be removed")
	}

	// 测试移除不存在的CIDR
	err = guardian.RemoveCIDR(ctx, "192.168.0.0/24")
	if err == nil {
		t.Error("RemoveCIDR should fail when CIDR does not exist")
	}

	// 测试上下文取消
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	guardian, _ = NewCIDRGuardian(ctx, nil, "192.168.0.0/24")
	err = guardian.RemoveCIDR(canceledCtx, "192.168.0.0/24")
	if err == nil {
		t.Error("RemoveCIDR should fail when context is canceled")
	}

	// 测试存储失败
	mockStorage := newMockIPStorage()
	guardian, _ = NewCIDRGuardian(ctx, mockStorage, "192.168.0.0/24")
	mockStorage.setFailure("IsIPAvailable", "mock failure")
	err = guardian.RemoveCIDR(ctx, "192.168.0.0/24")
	if err == nil {
		t.Error("RemoveCIDR should fail when storage fails")
	}
}

// TestCIDRGuardian_GetManagedCIDRs 测试获取管理的CIDR
func TestCIDRGuardian_GetManagedCIDRs(t *testing.T) {
	ctx := context.Background()
	guardian, _ := NewCIDRGuardian(ctx, nil, "192.168.0.0/24", "10.0.0.0/24")

	// 测试正常获取
	managed, err := guardian.GetManagedCIDRs(ctx)
	if err != nil {
		t.Errorf("GetManagedCIDRs should succeed: %v", err)
	}
	if len(managed) != 2 {
		t.Errorf("Expected 2 CIDRs, got %d", len(managed))
	}
	if _, exists := managed["192.168.0.0/24"]; !exists {
		t.Error("CIDR 192.168.0.0/24 should be managed")
	}
	if _, exists := managed["10.0.0.0/24"]; !exists {
		t.Error("CIDR 10.0.0.0/24 should be managed")
	}

	// 测试上下文取消
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = guardian.GetManagedCIDRs(canceledCtx)
	if err == nil {
		t.Error("GetManagedCIDRs should fail when context is canceled")
	}
}

// TestCIDRGuardian_AddSingleIP 测试添加单个IP
func TestCIDRGuardian_AddSingleIP(t *testing.T) {
	ctx := context.Background()
	guardian, _ := NewCIDRGuardian(ctx, nil)

	// 测试正常添加
	err := guardian.AddSingleIP(ctx, "192.168.0.1")
	if err != nil {
		t.Errorf("AddSingleIP should succeed: %v", err)
	}

	// 测试添加无效IP
	err = guardian.AddSingleIP(ctx, "invalid")
	if err == nil {
		t.Error("AddSingleIP should fail with invalid IP")
	}

	// 测试上下文取消
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	err = guardian.AddSingleIP(canceledCtx, "192.168.0.2")
	if err == nil {
		t.Error("AddSingleIP should fail when context is canceled")
	}

	// 测试存储失败
	mockStorage := newMockIPStorage()
	mockStorage.setFailure("AddIP", "mock failure")
	guardian, _ = NewCIDRGuardian(ctx, mockStorage)
	err = guardian.AddSingleIP(ctx, "192.168.0.1")
	if err == nil {
		t.Error("AddSingleIP should fail when storage fails")
	}
}

// TestCIDRGuardian_RemoveSingleIP 测试移除单个IP
func TestCIDRGuardian_RemoveSingleIP(t *testing.T) {
	ctx := context.Background()
	mockStorage := newMockIPStorage()
	guardian, _ := NewCIDRGuardian(ctx, mockStorage)

	// 添加一个IP
	mockStorage.available["192.168.0.1"] = true

	// 测试正常移除
	err := guardian.RemoveSingleIP(ctx, "192.168.0.1")
	if err != nil {
		t.Errorf("RemoveSingleIP should succeed: %v", err)
	}

	// 测试移除不存在的IP
	err = guardian.RemoveSingleIP(ctx, "192.168.0.1")
	if err == nil {
		t.Error("RemoveSingleIP should fail when IP is not available")
	}

	// 测试上下文取消
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	err = guardian.RemoveSingleIP(canceledCtx, "192.168.0.2")
	if err == nil {
		t.Error("RemoveSingleIP should fail when context is canceled")
	}

	// 测试存储失败
	mockStorage.available["192.168.0.1"] = true
	mockStorage.setFailure("RemoveIP", "mock failure")
	err = guardian.RemoveSingleIP(ctx, "192.168.0.1")
	if err == nil {
		t.Error("RemoveSingleIP should fail when storage fails")
	}
}

// TestCIDRGuardian_AllocateIP 测试分配IP
func TestCIDRGuardian_AllocateIP(t *testing.T) {
	ctx := context.Background()
	mockStorage := newMockIPStorage()
	guardian, _ := NewCIDRGuardian(ctx, mockStorage)

	// 添加一个IP
	mockStorage.available["192.168.0.1"] = true

	// 测试正常分配
	err := guardian.AllocateIP(ctx, "192.168.0.1", "test")
	if err != nil {
		t.Errorf("AllocateIP should succeed: %v", err)
	}

	// 测试分配不存在的IP
	err = guardian.AllocateIP(ctx, "192.168.0.2", "test")
	if err == nil {
		t.Error("AllocateIP should fail when IP is not available")
	}

	// 测试上下文取消
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	err = guardian.AllocateIP(canceledCtx, "192.168.0.1", "test")
	if err == nil {
		t.Error("AllocateIP should fail when context is canceled")
	}

	// 测试存储失败
	mockStorage.available["192.168.0.2"] = true
	mockStorage.setFailure("AllocateIP", "mock failure")
	err = guardian.AllocateIP(ctx, "192.168.0.2", "test")
	if err == nil {
		t.Error("AllocateIP should fail when storage fails")
	}
}

// TestCIDRGuardian_AllocateCIDR 测试分配CIDR
func TestCIDRGuardian_AllocateCIDR(t *testing.T) {
	ctx := context.Background()
	mockStorage := newMockIPStorage()
	guardian, _ := NewCIDRGuardian(ctx, mockStorage)

	// 准备足够的连续IP用于分配一个/30子网(4个IP)
	ips := []string{"192.168.0.0", "192.168.0.1", "192.168.0.2", "192.168.0.3"}
	for _, ip := range ips {
		mockStorage.available[ip] = true
	}

	// 测试正常分配
	cidr, err := guardian.AllocateCIDR(ctx, 30, "test")
	if err != nil {
		t.Errorf("AllocateCIDR should succeed: %v", err)
	}
	if cidr != "192.168.0.0/30" {
		t.Errorf("Expected 192.168.0.0/30, got %s", cidr)
	}

	// 测试无效的掩码位数
	_, err = guardian.AllocateCIDR(ctx, 33, "test")
	if err == nil {
		t.Error("AllocateCIDR should fail with invalid bits")
	}
	_, err = guardian.AllocateCIDR(ctx, -1, "test")
	if err == nil {
		t.Error("AllocateCIDR should fail with invalid bits")
	}

	// 测试没有足够的IP
	mockStorage.available = make(map[string]bool)
	mockStorage.available["192.168.0.4"] = true
	_, err = guardian.AllocateCIDR(ctx, 30, "test")
	if err == nil {
		t.Error("AllocateCIDR should fail when not enough IPs are available")
	}

	// 测试没有对齐的起始IP
	mockStorage.available = make(map[string]bool)
	for i := 1; i <= 4; i++ {
		mockStorage.available[fmt.Sprintf("192.168.0.%d", i)] = true
	}
	_, err = guardian.AllocateCIDR(ctx, 30, "test")
	if err == nil {
		t.Error("AllocateCIDR should fail when no aligned start IP is available")
	}

	// 测试上下文取消
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = guardian.AllocateCIDR(canceledCtx, 30, "test")
	if err == nil {
		t.Error("AllocateCIDR should fail when context is canceled")
	}

	// 测试获取IP列表失败
	mockStorage.setFailure("GetAvailableIPs", "mock failure")
	_, err = guardian.AllocateCIDR(ctx, 30, "test")
	if err == nil {
		t.Error("AllocateCIDR should fail when GetAvailableIPs fails")
	}

	// 测试检查IP可用性失败
	mockStorage.setFailure("GetAvailableIPs", "")
	mockStorage.available = make(map[string]bool)
	for _, ip := range ips {
		mockStorage.available[ip] = true
	}
	mockStorage.setFailure("IsIPAvailable", "mock failure")
	_, err = guardian.AllocateCIDR(ctx, 30, "test")
	if err == nil {
		t.Error("AllocateCIDR should fail when IsIPAvailable fails")
	}

	// 测试移除IP失败
	mockStorage.setFailure("IsIPAvailable", "")
	mockStorage.setFailure("RemoveIP", "mock failure")
	_, err = guardian.AllocateCIDR(ctx, 30, "test")
	if err == nil {
		t.Error("AllocateCIDR should fail when RemoveIP fails")
	}

	// 测试分配IP失败
	mockStorage.setFailure("RemoveIP", "")
	mockStorage.setFailure("AllocateIP", "mock failure")
	_, err = guardian.AllocateCIDR(ctx, 30, "test")
	if err == nil {
		t.Error("AllocateCIDR should fail when AllocateIP fails")
	}
}

// TestCIDRGuardian_ReleaseIP 测试释放IP
func TestCIDRGuardian_ReleaseIP(t *testing.T) {
	ctx := context.Background()
	mockStorage := newMockIPStorage()
	guardian, _ := NewCIDRGuardian(ctx, mockStorage)

	// 添加一个已分配的IP
	mockStorage.allocated["192.168.0.1"] = "test"

	// 测试正常释放
	err := guardian.ReleaseIP(ctx, "192.168.0.1")
	if err != nil {
		t.Errorf("ReleaseIP should succeed: %v", err)
	}

	// 测试释放不存在的IP
	err = guardian.ReleaseIP(ctx, "192.168.0.1")
	if err == nil {
		t.Error("ReleaseIP should fail when IP is not allocated")
	}

	// 测试上下文取消
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	err = guardian.ReleaseIP(canceledCtx, "192.168.0.1")
	if err == nil {
		t.Error("ReleaseIP should fail when context is canceled")
	}

	// 测试存储失败
	mockStorage.allocated["192.168.0.1"] = "test"
	mockStorage.setFailure("DeallocateIP", "mock failure")
	err = guardian.ReleaseIP(ctx, "192.168.0.1")
	if err == nil {
		t.Error("ReleaseIP should fail when storage fails")
	}
}

// TestCIDRGuardian_ReleaseCIDR 测试释放CIDR
func TestCIDRGuardian_ReleaseCIDR(t *testing.T) {
	ctx := context.Background()
	mockStorage := newMockIPStorage()
	guardian, _ := NewCIDRGuardian(ctx, mockStorage, "192.168.0.0/24")

	// 添加一个已分配的CIDR
	mockStorage.allocated["192.168.0.0"] = "192.168.0.0/28 - test"

	// 测试正常释放
	err := guardian.ReleaseCIDR(ctx, "192.168.0.0/28")
	if err != nil {
		t.Errorf("ReleaseCIDR should succeed: %v", err)
	}

	// 测试释放无效CIDR
	err = guardian.ReleaseCIDR(ctx, "invalid")
	if err == nil {
		t.Error("ReleaseCIDR should fail with invalid CIDR")
	}

	// 测试释放未分配的CIDR
	err = guardian.ReleaseCIDR(ctx, "192.168.0.0/28")
	if err == nil {
		t.Error("ReleaseCIDR should fail when CIDR is not allocated")
	}

	// 测试上下文取消
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	mockStorage.allocated["192.168.0.0"] = "192.168.0.0/28 - test"
	err = guardian.ReleaseCIDR(canceledCtx, "192.168.0.0/28")
	if err == nil {
		t.Error("ReleaseCIDR should fail when context is canceled")
	}

	// 测试获取已分配IP列表失败
	mockStorage.setFailure("GetAllocatedIPs", "mock failure")
	err = guardian.ReleaseCIDR(ctx, "192.168.0.0/28")
	if err == nil {
		t.Error("ReleaseCIDR should fail when GetAllocatedIPs fails")
	}

	// 测试添加IP失败
	mockStorage.setFailure("GetAllocatedIPs", "")
	mockStorage.allocated["192.168.0.0"] = "192.168.0.0/28 - test"
	mockStorage.setFailure("AddIP", "mock failure")
	err = guardian.ReleaseCIDR(ctx, "192.168.0.0/28")
	if err == nil {
		t.Error("ReleaseCIDR should fail when AddIP fails")
	}

	// 测试释放IP失败
	mockStorage.setFailure("AddIP", "")
	mockStorage.setFailure("DeallocateIP", "mock failure")
	err = guardian.ReleaseCIDR(ctx, "192.168.0.0/28")
	if err == nil {
		t.Error("ReleaseCIDR should fail when DeallocateIP fails")
	}
}

// TestCIDRGuardian_ExpandPool 测试扩展IP池
func TestCIDRGuardian_ExpandPool(t *testing.T) {
	ctx := context.Background()
	guardian, _ := NewCIDRGuardian(ctx, nil)

	// 测试正常扩展
	err := guardian.AddCIDR(ctx, "192.168.0.0/24", "initial")
	if err != nil {
		t.Fatalf("AddCIDR should succeed: %v", err)
	}

	// 测试扩展无效CIDR
	err = guardian.ExpandPool(ctx, "invalid")
	if err == nil {
		t.Error("ExpandPool should fail with invalid CIDR")
	}

	// 测试上下文取消
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	err = guardian.ExpandPool(canceledCtx, "10.0.0.0/24")
	if err == nil {
		t.Error("ExpandPool should fail when context is canceled")
	}

	// 测试获取已分配IP列表失败
	mockStorage := newMockIPStorage()
	guardian, _ = NewCIDRGuardian(ctx, mockStorage)
	mockStorage.setFailure("GetAllocatedIPs", "mock failure")
	err = guardian.ExpandPool(ctx, "192.168.0.0/24")
	if err == nil {
		t.Error("ExpandPool should fail when GetAllocatedIPs fails")
	}

	// 测试添加IP失败
	mockStorage.setFailure("GetAllocatedIPs", "")
	mockStorage.setFailure("AddIP", "mock failure")
	err = guardian.ExpandPool(ctx, "192.168.0.0/24")
	if err == nil {
		t.Error("ExpandPool should fail when AddIP fails")
	}
}

// TestCIDRGuardian_GetAvailableCIDRs 测试获取可用CIDR
func TestCIDRGuardian_GetAvailableCIDRs(t *testing.T) {
	ctx := context.Background()
	mockStorage := newMockIPStorage()
	guardian, _ := NewCIDRGuardian(ctx, mockStorage)

	// 添加一些IP
	mockStorage.available["192.168.0.1"] = true
	mockStorage.available["192.168.0.2"] = true
	mockStorage.available["10.0.0.1"] = true

	// 测试正常获取
	cidrs, err := guardian.GetAvailableCIDRs(ctx)
	if err != nil {
		t.Errorf("GetAvailableCIDRs should succeed: %v", err)
	}
	if len(cidrs) != 2 {
		t.Errorf("Expected 2 CIDRs, got %d", len(cidrs))
	}
	found192 := false
	found10 := false
	for _, cidr := range cidrs {
		if cidr == "192.168.0.0/24" {
			found192 = true
		}
		if cidr == "10.0.0.0/24" {
			found10 = true
		}
	}
	if !found192 || !found10 {
		t.Errorf("Expected to find both 192.168.0.0/24 and 10.0.0.0/24")
	}

	// 测试空列表
	mockStorage.available = make(map[string]bool)
	cidrs, err = guardian.GetAvailableCIDRs(ctx)
	if err != nil {
		t.Errorf("GetAvailableCIDRs should succeed: %v", err)
	}
	if cidrs != nil && len(cidrs) != 0 {
		t.Errorf("Expected empty list, got %v", cidrs)
	}

	// 测试上下文取消
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = guardian.GetAvailableCIDRs(canceledCtx)
	if err == nil {
		t.Error("GetAvailableCIDRs should fail when context is canceled")
	}

	// 测试获取IP列表失败
	mockStorage.setFailure("GetAvailableIPs", "mock failure")
	_, err = guardian.GetAvailableCIDRs(ctx)
	if err == nil {
		t.Error("GetAvailableCIDRs should fail when GetAvailableIPs fails")
	}
}

// TestCIDRGuardian_GetUsedCIDRs 测试获取已用CIDR
func TestCIDRGuardian_GetUsedCIDRs(t *testing.T) {
	ctx := context.Background()
	mockStorage := newMockIPStorage()
	guardian, _ := NewCIDRGuardian(ctx, mockStorage)

	// 添加一些已分配的CIDR
	mockStorage.allocated["192.168.0.0"] = "192.168.0.0/24 - web"
	mockStorage.allocated["10.0.0.0"] = "10.0.0.0/24 - db"
	mockStorage.allocated["172.16.0.1"] = "single IP"

	// 测试正常获取
	cidrs, err := guardian.GetUsedCIDRs(ctx)
	if err != nil {
		t.Errorf("GetUsedCIDRs should succeed: %v", err)
	}
	if len(cidrs) != 2 {
		t.Errorf("Expected 2 CIDRs, got %d", len(cidrs))
	}
	if desc, exists := cidrs["192.168.0.0/24"]; !exists || desc != "web" {
		t.Errorf("Expected 192.168.0.0/24 with description 'web'")
	}
	if desc, exists := cidrs["10.0.0.0/24"]; !exists || desc != "db" {
		t.Errorf("Expected 10.0.0.0/24 with description 'db'")
	}

	// 测试空列表
	mockStorage.allocated = make(map[string]string)
	cidrs, err = guardian.GetUsedCIDRs(ctx)
	if err != nil {
		t.Errorf("GetUsedCIDRs should succeed: %v", err)
	}
	if len(cidrs) != 0 {
		t.Errorf("Expected empty map, got %v", cidrs)
	}

	// 测试上下文取消
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = guardian.GetUsedCIDRs(canceledCtx)
	if err == nil {
		t.Error("GetUsedCIDRs should fail when context is canceled")
	}

	// 测试获取已分配IP列表失败
	mockStorage.setFailure("GetAllocatedIPs", "mock failure")
	_, err = guardian.GetUsedCIDRs(ctx)
	if err == nil {
		t.Error("GetUsedCIDRs should fail when GetAllocatedIPs fails")
	}
}

// TestCIDRGuardian_AvailableCount 测试获取可用IP数量
func TestCIDRGuardian_AvailableCount(t *testing.T) {
	ctx := context.Background()
	mockStorage := newMockIPStorage()
	guardian, _ := NewCIDRGuardian(ctx, mockStorage)

	// 添加一些IP
	mockStorage.available["192.168.0.1"] = true
	mockStorage.available["192.168.0.2"] = true

	// 测试正常获取
	count, err := guardian.AvailableCount(ctx)
	if err != nil {
		t.Errorf("AvailableCount should succeed: %v", err)
	}
	if count != 2 {
		t.Errorf("Expected 2, got %d", count)
	}

	// 测试上下文取消
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = guardian.AvailableCount(canceledCtx)
	if err == nil {
		t.Error("AvailableCount should fail when context is canceled")
	}

	// 测试存储失败
	mockStorage.setFailure("AvailableCount", "mock failure")
	_, err = guardian.AvailableCount(ctx)
	if err == nil {
		t.Error("AvailableCount should fail when storage fails")
	}
}

// TestCIDRGuardian_AllocatedCount 测试获取已分配IP数量
func TestCIDRGuardian_AllocatedCount(t *testing.T) {
	ctx := context.Background()
	mockStorage := newMockIPStorage()
	guardian, _ := NewCIDRGuardian(ctx, mockStorage)

	// 添加一些已分配的IP
	mockStorage.allocated["192.168.0.1"] = "test1"
	mockStorage.allocated["192.168.0.2"] = "test2"

	// 测试正常获取
	count, err := guardian.AllocatedCount(ctx)
	if err != nil {
		t.Errorf("AllocatedCount should succeed: %v", err)
	}
	if count != 2 {
		t.Errorf("Expected 2, got %d", count)
	}

	// 测试上下文取消
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = guardian.AllocatedCount(canceledCtx)
	if err == nil {
		t.Error("AllocatedCount should fail when context is canceled")
	}

	// 测试存储失败
	mockStorage.setFailure("AllocatedCount", "mock failure")
	_, err = guardian.AllocatedCount(ctx)
	if err == nil {
		t.Error("AllocatedCount should fail when storage fails")
	}
}

// TestCIDRGuardian_String 测试获取字符串表示
func TestCIDRGuardian_String(t *testing.T) {
	ctx := context.Background()
	mockStorage := newMockIPStorage()
	guardian, _ := NewCIDRGuardian(ctx, mockStorage, "192.168.0.0/24")

	// 添加一些已分配的CIDR
	mockStorage.allocated["192.168.0.0"] = "192.168.0.0/28 - web"
	mockStorage.available["192.168.0.100"] = true

	// 测试正常获取
	str, err := guardian.String(ctx)
	if err != nil {
		t.Errorf("String should succeed: %v", err)
	}
	if !strings.Contains(str, "CIDRGuardian 状态") {
		t.Error("String output should contain title")
	}
	if !strings.Contains(str, "192.168.0.0/24") {
		t.Error("String output should contain managed CIDR")
	}
	if !strings.Contains(str, "192.168.0.0/28") {
		t.Error("String output should contain allocated CIDR")
	}

	// 测试失败情况
	// GetManagedCIDRs 失败
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = guardian.String(canceledCtx)
	if err == nil {
		t.Error("String should fail when context is canceled")
	}

	// GetUsedCIDRs 失败
	mockStorage.setFailure("GetAllocatedIPs", "mock failure")
	_, err = guardian.String(ctx)
	if err == nil {
		t.Error("String should fail when GetUsedCIDRs fails")
	}

	// AvailableCount 失败
	mockStorage.setFailure("GetAllocatedIPs", "")
	mockStorage.setFailure("AvailableCount", "mock failure")
	_, err = guardian.String(ctx)
	if err == nil {
		t.Error("String should fail when AvailableCount fails")
	}

	// AllocatedCount 失败
	mockStorage.setFailure("AvailableCount", "")
	mockStorage.setFailure("AllocatedCount", "mock failure")
	_, err = guardian.String(ctx)
	if err == nil {
		t.Error("String should fail when AllocatedCount fails")
	}

	// GetAvailableCIDRs 失败
	mockStorage.setFailure("AllocatedCount", "")
	mockStorage.setFailure("GetAvailableIPs", "mock failure")
	_, err = guardian.String(ctx)
	if err == nil {
		t.Error("String should fail when GetAvailableCIDRs fails")
	}
}

// 测试辅助函数
func TestHelperFunctions(t *testing.T) {
	// 测试 cloneIP
	ip := net.ParseIP("192.168.0.1")
	clone := cloneIP(ip)
	if !ip.Equal(clone) {
		t.Error("cloneIP should create an identical copy")
	}

	// 修改克隆不应影响原始IP
	clone[len(clone)-1] = 2
	if ip.Equal(clone) {
		t.Error("Modifying clone should not affect original IP")
	}

	// 测试 nextIP
	ip = net.ParseIP("192.168.0.1")
	nextIP(ip)
	if !ip.Equal(net.ParseIP("192.168.0.2")) {
		t.Error("nextIP should increment IP correctly")
	}

	// 测试边界情况
	ip = net.ParseIP("192.168.0.255")
	nextIP(ip)
	if !ip.Equal(net.ParseIP("192.168.1.0")) {
		t.Error("nextIP should handle overflow correctly")
	}
}

// TestCIDRGuardian_GetNextAvailableIP 测试获取下一个可用IP
func TestCIDRGuardian_GetNextAvailableIP(t *testing.T) {
	ctx := context.Background()
	mockStorage := newMockIPStorage()
	guardian, _ := NewCIDRGuardian(ctx, mockStorage)

	// 添加两个IP
	mockStorage.available["192.168.0.1"] = true
	mockStorage.available["192.168.0.2"] = true

	// 测试正常获取
	ip, err := guardian.GetNextAvailableIP(ctx, "test")
	if err != nil {
		t.Errorf("GetNextAvailableIP should succeed: %v", err)
	}
	if ip != "192.168.0.1" && ip != "192.168.0.2" {
		t.Errorf("Expected 192.168.0.1 or 192.168.0.2, got %s", ip)
	}

	// 测试没有可用IP
	mockStorage.available = make(map[string]bool)
	_, err = guardian.GetNextAvailableIP(ctx, "test")
	if err == nil {
		t.Error("GetNextAvailableIP should fail when no IP is available")
	}

	// 测试上下文取消
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = guardian.GetNextAvailableIP(canceledCtx, "test")
	if err == nil {
		t.Error("GetNextAvailableIP should fail when context is canceled")
	}

	// 测试获取IP列表失败
	mockStorage.setFailure("GetAvailableIPs", "mock failure")
	_, err = guardian.GetNextAvailableIP(ctx, "test")
	if err == nil {
		t.Error("GetNextAvailableIP should fail when GetAvailableIPs fails")
	}

	// 测试分配IP失败
	mockStorage.setFailure("GetAvailableIPs", "")
	mockStorage.available["192.168.0.1"] = true
	mockStorage.setFailure("AllocateIP", "mock failure")
	_, err = guardian.GetNextAvailableIP(ctx, "test")
	if err == nil {
		t.Error("GetNextAvailableIP should fail when AllocateIP fails")
	}
}

// setupMockDB 创建一个带有 Mock 的数据库连接
func setupMockDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock, *SQLIPStorage) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	if err != nil {
		t.Fatalf("无法创建 sqlmock: %v", err)
	}

	storage := &SQLIPStorage{
		db:         db,
		driverName: "mysql", // 使用 MySQL 语法测试
	}

	return db, mock, storage
}

// TestNewSQLIPStorage_Integration 集成测试新建 SQL 存储
// 这个测试需要实际的数据库连接，如果环境变量未设置则跳过
func TestNewSQLIPStorage_Integration(t *testing.T) {
	// 检查是否有环境变量来进行集成测试
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("跳过集成测试，未设置 TEST_MYSQL_DSN 环境变量")
	}

	ctx := context.Background()
	config := SQLConfig{
		DriverName:      "mysql",
		DataSourceName:  dsn,
		MaxOpenConns:    5,
		MaxIdleConns:    2,
		ConnMaxLifetime: time.Minute,
	}

	storage, err := NewSQLIPStorage(ctx, config)
	if err != nil {
		t.Fatalf("创建 SQLIPStorage 失败: %v", err)
	}
	defer storage.Close()

	// 简单验证存储是否正常工作
	ip := "192.168.1.1"

	// 清理测试数据（如果存在）
	_, _ = storage.db.ExecContext(ctx, "DELETE FROM ip_available WHERE ip = ?", ip)
	_, _ = storage.db.ExecContext(ctx, "DELETE FROM ip_allocated WHERE ip = ?", ip)

	// 添加一个 IP
	if err := storage.AddIP(ctx, ip); err != nil {
		t.Errorf("AddIP 失败: %v", err)
	}

	// 检查是否可用
	available, err := storage.IsIPAvailable(ctx, ip)
	if err != nil {
		t.Errorf("IsIPAvailable 失败: %v", err)
	}
	if !available {
		t.Errorf("IP 应该可用")
	}

	// 分配 IP
	if err := storage.AllocateIP(ctx, ip, "test"); err != nil {
		t.Errorf("AllocateIP 失败: %v", err)
	}

	// 检查是否已分配
	allocated, err := storage.GetAllocatedIPs(ctx)
	if err != nil {
		t.Errorf("GetAllocatedIPs 失败: %v", err)
	}
	if desc, ok := allocated[ip]; !ok || desc != "test" {
		t.Errorf("IP 应该已分配且描述为 'test'")
	}

	// 释放 IP
	if err := storage.DeallocateIP(ctx, ip); err != nil {
		t.Errorf("DeallocateIP 失败: %v", err)
	}

	// 清理
	if err := storage.RemoveIP(ctx, ip); err != nil {
		t.Errorf("RemoveIP 失败: %v", err)
	}
}

// TestSQLIPStorage_initTables 测试表初始化
func TestSQLIPStorage_initTables(t *testing.T) {
	db, mock, storage := setupMockDB(t)
	defer db.Close()

	ctx := context.Background()

	// 预期 MySQL 表创建查询
	mock.ExpectExec(`CREATE TABLE IF NOT EXISTS ip_available (
			ip VARCHAR(45) PRIMARY KEY,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		) ENGINE=InnoDB;`).WillReturnResult(sqlmock.NewResult(0, 0))

	mock.ExpectExec(`CREATE TABLE IF NOT EXISTS ip_allocated (
			ip VARCHAR(45) PRIMARY KEY,
			description TEXT,
			allocated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		) ENGINE=InnoDB;`).WillReturnResult(sqlmock.NewResult(0, 0))

	// 执行初始化
	err := storage.initTables(ctx)
	if err != nil {
		t.Errorf("initTables 失败: %v", err)
	}

	// 验证所有预期的 SQL 语句已被执行
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("有未满足的预期: %s", err)
	}
}

// TestSQLIPStorage_AddIP 测试添加 IP
func TestSQLIPStorage_AddIP(t *testing.T) {
	db, mock, storage := setupMockDB(t)
	defer db.Close()

	ctx := context.Background()
	ip := "192.168.1.1"

	// 预期检查 IP 是否已分配
	mock.ExpectBegin()
	checkRows := sqlmock.NewRows([]string{"count"}).AddRow(0)
	mock.ExpectQuery("SELECT COUNT(*) FROM ip_allocated WHERE ip = ?").
		WithArgs(ip).
		WillReturnRows(checkRows)

	// 预期添加到可用池
	mock.ExpectExec("INSERT INTO ip_available (ip) VALUES (?) ON DUPLICATE KEY UPDATE ip = ip").
		WithArgs(ip).
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectCommit()

	// 执行添加
	err := storage.AddIP(ctx, ip)
	if err != nil {
		t.Errorf("AddIP 失败: %v", err)
	}

	// 验证所有预期的 SQL 语句已被执行
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("有未满足的预期: %s", err)
	}

	// 测试 IP 已分配的情况
	mock.ExpectBegin()
	allocatedRows := sqlmock.NewRows([]string{"count"}).AddRow(1)
	mock.ExpectQuery("SELECT COUNT(*) FROM ip_allocated WHERE ip = ?").
		WithArgs(ip).
		WillReturnRows(allocatedRows)
	mock.ExpectRollback()

	// 执行添加
	err = storage.AddIP(ctx, ip)
	if err == nil {
		t.Error("当 IP 已分配时，AddIP 应该失败")
	}

	// 验证所有预期的 SQL 语句已被执行
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("有未满足的预期: %s", err)
	}
}

// TestSQLIPStorage_RemoveIP 测试移除 IP
func TestSQLIPStorage_RemoveIP(t *testing.T) {
	db, mock, storage := setupMockDB(t)
	defer db.Close()

	ctx := context.Background()
	ip := "192.168.1.1"

	// 预期检查 IP 是否可用
	mock.ExpectBegin()
	checkRows := sqlmock.NewRows([]string{"count"}).AddRow(1)
	mock.ExpectQuery("SELECT COUNT(*) FROM ip_available WHERE ip = ?").
		WithArgs(ip).
		WillReturnRows(checkRows)

	// 预期从可用池中移除
	mock.ExpectExec("DELETE FROM ip_available WHERE ip = ?").
		WithArgs(ip).
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectCommit()

	// 执行移除
	err := storage.RemoveIP(ctx, ip)
	if err != nil {
		t.Errorf("RemoveIP 失败: %v", err)
	}

	// 验证所有预期的 SQL 语句已被执行
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("有未满足的预期: %s", err)
	}

	// 测试 IP 不可用的情况
	mock.ExpectBegin()
	notAvailableRows := sqlmock.NewRows([]string{"count"}).AddRow(0)
	mock.ExpectQuery("SELECT COUNT(*) FROM ip_available WHERE ip = ?").
		WithArgs(ip).
		WillReturnRows(notAvailableRows)
	mock.ExpectRollback()

	// 执行移除
	err = storage.RemoveIP(ctx, ip)
	if err == nil {
		t.Error("当 IP 不可用时，RemoveIP 应该失败")
	}

	// 验证所有预期的 SQL 语句已被执行
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("有未满足的预期: %s", err)
	}
}

// TestSQLIPStorage_IsIPAvailable 测试检查 IP 是否可用
func TestSQLIPStorage_IsIPAvailable(t *testing.T) {
	db, mock, storage := setupMockDB(t)
	defer db.Close()

	ctx := context.Background()
	ip := "192.168.1.1"

	// 测试 IP 可用的情况
	availableRows := sqlmock.NewRows([]string{"count"}).AddRow(1)
	mock.ExpectQuery("SELECT COUNT(*) FROM ip_available WHERE ip = ?").
		WithArgs(ip).
		WillReturnRows(availableRows)

	available, err := storage.IsIPAvailable(ctx, ip)
	if err != nil {
		t.Errorf("IsIPAvailable 失败: %v", err)
	}
	if !available {
		t.Error("IP 应该可用")
	}

	// 测试 IP 不可用的情况
	notAvailableRows := sqlmock.NewRows([]string{"count"}).AddRow(0)
	mock.ExpectQuery("SELECT COUNT(*) FROM ip_available WHERE ip = ?").
		WithArgs(ip).
		WillReturnRows(notAvailableRows)

	available, err = storage.IsIPAvailable(ctx, ip)
	if err != nil {
		t.Errorf("IsIPAvailable 失败: %v", err)
	}
	if available {
		t.Error("IP 应该不可用")
	}

	// 验证所有预期的 SQL 语句已被执行
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("有未满足的预期: %s", err)
	}
}

// TestSQLIPStorage_GetAvailableIPs 测试获取可用 IP 列表
func TestSQLIPStorage_GetAvailableIPs(t *testing.T) {
	db, mock, storage := setupMockDB(t)
	defer db.Close()

	ctx := context.Background()
	expectedIPs := []string{"192.168.1.1", "192.168.1.2", "192.168.1.3"}

	// 预期查询
	rows := sqlmock.NewRows([]string{"ip"})
	for _, ip := range expectedIPs {
		rows.AddRow(ip)
	}
	mock.ExpectQuery("SELECT ip FROM ip_available ORDER BY ip").WillReturnRows(rows)

	// 获取可用 IP
	ips, err := storage.GetAvailableIPs(ctx)
	if err != nil {
		t.Errorf("GetAvailableIPs 失败: %v", err)
	}

	// 验证结果
	if !reflect.DeepEqual(ips, expectedIPs) {
		t.Errorf("预期 %v, 得到 %v", expectedIPs, ips)
	}

	// 测试空列表
	emptyRows := sqlmock.NewRows([]string{"ip"})
	mock.ExpectQuery("SELECT ip FROM ip_available ORDER BY ip").WillReturnRows(emptyRows)

	ips, err = storage.GetAvailableIPs(ctx)
	if err != nil {
		t.Errorf("GetAvailableIPs 失败: %v", err)
	}
	if len(ips) != 0 {
		t.Errorf("预期空列表, 得到 %v", ips)
	}

	// 验证所有预期的 SQL 语句已被执行
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("有未满足的预期: %s", err)
	}
}

// TestSQLIPStorage_AllocateIP 测试分配 IP
func TestSQLIPStorage_AllocateIP(t *testing.T) {
	db, mock, storage := setupMockDB(t)
	defer db.Close()

	ctx := context.Background()
	ip := "192.168.1.1"
	description := "测试分配"

	// 预期事务和查询
	mock.ExpectBegin()
	availableRows := sqlmock.NewRows([]string{"count"}).AddRow(1)
	mock.ExpectQuery("SELECT COUNT(*) FROM ip_available WHERE ip = ?").
		WithArgs(ip).
		WillReturnRows(availableRows)

	// 预期从可用池中移除
	mock.ExpectExec("DELETE FROM ip_available WHERE ip = ?").
		WithArgs(ip).
		WillReturnResult(sqlmock.NewResult(0, 1))

	// 预期添加到已分配池
	mock.ExpectExec("INSERT INTO ip_allocated (ip, description) VALUES (?, ?)").
		WithArgs(ip, description).
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectCommit()

	// 执行分配
	err := storage.AllocateIP(ctx, ip, description)
	if err != nil {
		t.Errorf("AllocateIP 失败: %v", err)
	}

	// 测试 IP 不可用的情况
	mock.ExpectBegin()
	notAvailableRows := sqlmock.NewRows([]string{"count"}).AddRow(0)
	mock.ExpectQuery("SELECT COUNT(*) FROM ip_available WHERE ip = ?").
		WithArgs(ip).
		WillReturnRows(notAvailableRows)
	mock.ExpectRollback()

	err = storage.AllocateIP(ctx, ip, description)
	if err == nil {
		t.Error("当 IP 不可用时，AllocateIP 应该失败")
	}

	// 验证所有预期的 SQL 语句已被执行
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("有未满足的预期: %s", err)
	}
}

// TestSQLIPStorage_DeallocateIP 测试释放 IP
func TestSQLIPStorage_DeallocateIP(t *testing.T) {
	db, mock, storage := setupMockDB(t)
	defer db.Close()

	ctx := context.Background()
	ip := "192.168.1.1"

	// 预期事务和查询
	mock.ExpectBegin()
	allocatedRows := sqlmock.NewRows([]string{"count"}).AddRow(1)
	mock.ExpectQuery("SELECT COUNT(*) FROM ip_allocated WHERE ip = ?").
		WithArgs(ip).
		WillReturnRows(allocatedRows)

	// 预期从已分配池中移除
	mock.ExpectExec("DELETE FROM ip_allocated WHERE ip = ?").
		WithArgs(ip).
		WillReturnResult(sqlmock.NewResult(0, 1))

	// 预期添加到可用池
	mock.ExpectExec("INSERT INTO ip_available (ip) VALUES (?)").
		WithArgs(ip).
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectCommit()

	// 执行释放
	err := storage.DeallocateIP(ctx, ip)
	if err != nil {
		t.Errorf("DeallocateIP 失败: %v", err)
	}

	// 测试 IP 未分配的情况
	mock.ExpectBegin()
	notAllocatedRows := sqlmock.NewRows([]string{"count"}).AddRow(0)
	mock.ExpectQuery("SELECT COUNT(*) FROM ip_allocated WHERE ip = ?").
		WithArgs(ip).
		WillReturnRows(notAllocatedRows)
	mock.ExpectRollback()

	err = storage.DeallocateIP(ctx, ip)
	if err == nil {
		t.Error("当 IP 未分配时，DeallocateIP 应该失败")
	}

	// 验证所有预期的 SQL 语句已被执行
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("有未满足的预期: %s", err)
	}
}

// TestSQLIPStorage_GetAllocatedIPs 测试获取已分配 IP 列表
func TestSQLIPStorage_GetAllocatedIPs(t *testing.T) {
	db, mock, storage := setupMockDB(t)
	defer db.Close()

	ctx := context.Background()
	expectedAllocated := map[string]string{
		"192.168.1.1": "web 服务器",
		"192.168.1.2": "数据库服务器",
		"192.168.1.3": "缓存服务器",
	}

	// 预期查询
	rows := sqlmock.NewRows([]string{"ip", "description"})
	for ip, desc := range expectedAllocated {
		rows.AddRow(ip, desc)
	}
	mock.ExpectQuery("SELECT ip, description FROM ip_allocated").WillReturnRows(rows)

	// 获取已分配 IP
	allocated, err := storage.GetAllocatedIPs(ctx)
	if err != nil {
		t.Errorf("GetAllocatedIPs 失败: %v", err)
	}

	// 验证结果
	if !reflect.DeepEqual(allocated, expectedAllocated) {
		t.Errorf("预期 %v, 得到 %v", expectedAllocated, allocated)
	}

	// 测试空列表
	emptyRows := sqlmock.NewRows([]string{"ip", "description"})
	mock.ExpectQuery("SELECT ip, description FROM ip_allocated").WillReturnRows(emptyRows)

	allocated, err = storage.GetAllocatedIPs(ctx)
	if err != nil {
		t.Errorf("GetAllocatedIPs 失败: %v", err)
	}
	if len(allocated) != 0 {
		t.Errorf("预期空映射, 得到 %v", allocated)
	}

	// 验证所有预期的 SQL 语句已被执行
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("有未满足的预期: %s", err)
	}
}

// TestSQLIPStorage_AvailableCount 测试获取可用 IP 数量
func TestSQLIPStorage_AvailableCount(t *testing.T) {
	db, mock, storage := setupMockDB(t)
	defer db.Close()

	ctx := context.Background()
	expectedCount := 42

	// 预期查询
	rows := sqlmock.NewRows([]string{"count"}).AddRow(expectedCount)
	mock.ExpectQuery("SELECT COUNT(*) FROM ip_available").WillReturnRows(rows)

	// 获取数量
	count, err := storage.AvailableCount(ctx)
	if err != nil {
		t.Errorf("AvailableCount 失败: %v", err)
	}

	// 验证结果
	if count != expectedCount {
		t.Errorf("预期 %d, 得到 %d", expectedCount, count)
	}

	// 验证所有预期的 SQL 语句已被执行
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("有未满足的预期: %s", err)
	}
}

// TestSQLIPStorage_AllocatedCount 测试获取已分配 IP 数量
func TestSQLIPStorage_AllocatedCount(t *testing.T) {
	db, mock, storage := setupMockDB(t)
	defer db.Close()

	ctx := context.Background()
	expectedCount := 17

	// 预期查询
	rows := sqlmock.NewRows([]string{"count"}).AddRow(expectedCount)
	mock.ExpectQuery("SELECT COUNT(*) FROM ip_allocated").WillReturnRows(rows)

	// 获取数量
	count, err := storage.AllocatedCount(ctx)
	if err != nil {
		t.Errorf("AllocatedCount 失败: %v", err)
	}

	// 验证结果
	if count != expectedCount {
		t.Errorf("预期 %d, 得到 %d", expectedCount, count)
	}

	// 验证所有预期的 SQL 语句已被执行
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("有未满足的预期: %s", err)
	}
}

// TestSQLIPStorage_CanceledContext 测试上下文取消
func TestSQLIPStorage_CanceledContext(t *testing.T) {
	_, _, storage := setupMockDB(t)

	// 创建已取消的上下文
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// 测试所有方法，确保它们都处理上下文取消
	if _, err := storage.IsIPAvailable(ctx, "192.168.1.1"); err != context.Canceled {
		t.Errorf("IsIPAvailable 应该返回上下文取消错误，得到: %v", err)
	}

	if err := storage.AddIP(ctx, "192.168.1.1"); err != context.Canceled {
		t.Errorf("AddIP 应该返回上下文取消错误，得到: %v", err)
	}

	if err := storage.RemoveIP(ctx, "192.168.1.1"); err != context.Canceled {
		t.Errorf("RemoveIP 应该返回上下文取消错误，得到: %v", err)
	}

	if _, err := storage.GetAvailableIPs(ctx); err != context.Canceled {
		t.Errorf("GetAvailableIPs 应该返回上下文取消错误，得到: %v", err)
	}

	if err := storage.AllocateIP(ctx, "192.168.1.1", "test"); err != context.Canceled {
		t.Errorf("AllocateIP 应该返回上下文取消错误，得到: %v", err)
	}

	if err := storage.DeallocateIP(ctx, "192.168.1.1"); err != context.Canceled {
		t.Errorf("DeallocateIP 应该返回上下文取消错误，得到: %v", err)
	}

	if _, err := storage.GetAllocatedIPs(ctx); err != context.Canceled {
		t.Errorf("GetAllocatedIPs 应该返回上下文取消错误，得到: %v", err)
	}

	if _, err := storage.AvailableCount(ctx); err != context.Canceled {
		t.Errorf("AvailableCount 应该返回上下文取消错误，得到: %v", err)
	}

	if _, err := storage.AllocatedCount(ctx); err != context.Canceled {
		t.Errorf("AllocatedCount 应该返回上下文取消错误，得到: %v", err)
	}
}

// TestSQLIPStorage_ErrorHandling 测试错误处理
func TestSQLIPStorage_ErrorHandling(t *testing.T) {
	db, mock, storage := setupMockDB(t)
	defer db.Close()

	ctx := context.Background()
	ip := "192.168.1.1"

	// 测试查询错误
	databaseError := fmt.Errorf("数据库连接失败")
	mock.ExpectQuery("SELECT COUNT(*) FROM ip_available WHERE ip = ?").
		WithArgs(ip).
		WillReturnError(databaseError)

	_, err := storage.IsIPAvailable(ctx, ip)
	if err == nil {
		t.Error("当查询失败时，IsIPAvailable 应该返回错误")
	}

	// 测试事务错误
	mock.ExpectBegin().WillReturnError(databaseError)

	err = storage.AddIP(ctx, ip)
	if err == nil {
		t.Error("当事务开始失败时，AddIP 应该返回错误")
	}

	// 验证所有预期的 SQL 语句已被执行
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("有未满足的预期: %s", err)
	}
}
