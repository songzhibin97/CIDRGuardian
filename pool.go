package CIDRGuardian

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
)

// CIDRInfo 存储 CIDR 的信息
type CIDRInfo struct {
	CIDR        string     // CIDR 字符串表示
	Description string     // CIDR 描述
	IPNet       *net.IPNet // CIDR 的网络表示
}

// CIDRGuardian 定义一个增强的 IP 池结构体，支持多 CIDR 管理
type CIDRGuardian struct {
	mu           sync.RWMutex
	storage      IPStorage
	managedCIDRs map[string]*CIDRInfo // 管理的所有 CIDR 信息
}

// NewCIDRGuardian 初始化一个新的 CIDRGuardian
// 可以传入零个或多个初始 CIDR
func NewCIDRGuardian(ctx context.Context, storage IPStorage, initialCIDRs ...string) (*CIDRGuardian, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if storage == nil {
		storage = NewMemoryIPStorage()
	}

	guardian := &CIDRGuardian{
		storage:      storage,
		managedCIDRs: make(map[string]*CIDRInfo),
	}

	// 初始化传入的所有 CIDR
	for _, cidr := range initialCIDRs {
		if err := guardian.AddCIDR(ctx, cidr, "初始 CIDR"); err != nil {
			return nil, fmt.Errorf("添加初始 CIDR %s 失败: %v", cidr, err)
		}
	}

	return guardian, nil
}

// AddCIDR 添加一个新的 CIDR 到管理池
func (g *CIDRGuardian) AddCIDR(ctx context.Context, cidr, description string) error {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return err
	}

	// 解析CIDR
	ip, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("无效的CIDR格式 %s: %v", cidr, err)
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	// 检查是否已存在相同的 CIDR
	if _, exists := g.managedCIDRs[cidr]; exists {
		return fmt.Errorf("CIDR %s 已在管理池中", cidr)
	}

	// 将 CIDR 中的所有 IP 添加到可用池
	ipList := []net.IP{}
	for ip := cloneIP(ip.Mask(ipNet.Mask)); ipNet.Contains(ip); nextIP(ip) {
		ipList = append(ipList, cloneIP(ip))
	}

	// 逐个添加IP，如果失败则回滚
	addedIPs := []string{}
	for _, ip := range ipList {
		// 检查上下文是否已取消
		if err := ctx.Err(); err != nil {
			// 回滚已添加的IP
			for _, addedIP := range addedIPs {
				_ = g.storage.RemoveIP(ctx, addedIP)
			}
			return err
		}

		ipStr := ip.String()
		err := g.storage.AddIP(ctx, ipStr)
		if err != nil {
			// 如果不是"IP已存在"错误，则需要回滚
			if !strings.Contains(err.Error(), "已被分配") && !strings.Contains(err.Error(), "already allocated") {
				// 回滚已添加的IP
				for _, addedIP := range addedIPs {
					_ = g.storage.RemoveIP(ctx, addedIP)
				}
				return err
			}
		} else {
			addedIPs = append(addedIPs, ipStr)
		}
	}

	// 保存 CIDR 信息
	g.managedCIDRs[cidr] = &CIDRInfo{
		CIDR:        cidr,
		Description: description,
		IPNet:       ipNet,
	}

	return nil
}

// removeCIDRWithoutLock 内部方法，从管理池中移除 CIDR，不加锁
func (g *CIDRGuardian) removeCIDRWithoutLock(ctx context.Context, cidr string) error {
	cidrInfo, exists := g.managedCIDRs[cidr]
	if !exists {
		return fmt.Errorf("CIDR %s 不在管理池中", cidr)
	}

	// 从可用池中移除 CIDR 中的 IP
	for ip := cloneIP(cidrInfo.IPNet.IP.Mask(cidrInfo.IPNet.Mask)); cidrInfo.IPNet.Contains(ip); nextIP(ip) {
		// 检查上下文是否已取消
		if err := ctx.Err(); err != nil {
			return err
		}

		// 尝试移除 IP，忽略不存在的 IP 错误
		available, err := g.storage.IsIPAvailable(ctx, ip.String())
		if err != nil {
			return err
		}

		if available {
			if err := g.storage.RemoveIP(ctx, ip.String()); err != nil {
				return err
			}
		}
	}

	// 从管理的 CIDR 列表中移除
	delete(g.managedCIDRs, cidr)

	return nil
}

// RemoveCIDR 从管理池中移除一个 CIDR
func (g *CIDRGuardian) RemoveCIDR(ctx context.Context, cidr string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.removeCIDRWithoutLock(ctx, cidr)
}

// GetManagedCIDRs 获取所有管理的 CIDR 及其描述
func (g *CIDRGuardian) GetManagedCIDRs(ctx context.Context) (map[string]string, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	g.mu.RLock()
	defer g.mu.RUnlock()

	result := make(map[string]string)
	for cidr, info := range g.managedCIDRs {
		result[cidr] = info.Description
	}

	return result, nil
}

// cloneIP 克隆一个IP
func cloneIP(ip net.IP) net.IP {
	clone := make(net.IP, len(ip))
	copy(clone, ip)
	return clone
}

// nextIP 计算下一个IP
func nextIP(ip net.IP) {
	for i := len(ip) - 1; i >= 0; i-- {
		ip[i]++
		if ip[i] > 0 {
			break
		}
	}
}

// AddSingleIP 添加单个IP到管理池
func (g *CIDRGuardian) AddSingleIP(ctx context.Context, ip string) error {
	// 解析 IP
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return fmt.Errorf("无效的IP地址格式: %s", ip)
	}

	// 检查 IP 是否在任何管理的 CIDR 范围内
	g.mu.RLock()
	defer g.mu.RUnlock()

	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return err
	}

	// 直接添加到可用池
	return g.storage.AddIP(ctx, ip)
}

// RemoveSingleIP 从管理池中移除单个IP
func (g *CIDRGuardian) RemoveSingleIP(ctx context.Context, ip string) error {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return err
	}

	return g.storage.RemoveIP(ctx, ip)
}

// ExpandPool 扩展IP池，添加新的CIDR
func (g *CIDRGuardian) ExpandPool(ctx context.Context, cidr string) error {
	// 解析新CIDR
	_, newNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("无效的CIDR格式: %v", err)
	}

	// 将新CIDR中的所有IP添加到可用池
	for ip := cloneIP(newNet.IP.Mask(newNet.Mask)); newNet.Contains(ip); nextIP(ip) {
		// 检查上下文是否已取消
		if err := ctx.Err(); err != nil {
			return err
		}

		// 检查IP是否已在任何已分配的CIDR中
		allocated, err := g.storage.GetAllocatedIPs(ctx)
		if err != nil {
			return err
		}

		if _, exists := allocated[ip.String()]; !exists {
			if err := g.storage.AddIP(ctx, ip.String()); err != nil {
				return err
			}
		}
	}

	// 将新CIDR添加到管理池中
	return g.AddCIDR(ctx, cidr, "扩展的网段")
}

// AllocateIP 分配一个指定的IP
func (g *CIDRGuardian) AllocateIP(ctx context.Context, ipStr string, description string) error {
	return g.storage.AllocateIP(ctx, ipStr, description)
}

// GetNextAvailableIP 获取下一个可用的IP
func (g *CIDRGuardian) GetNextAvailableIP(ctx context.Context, description string) (string, error) {
	ips, err := g.storage.GetAvailableIPs(ctx)
	if err != nil {
		return "", err
	}

	if len(ips) == 0 {
		return "", fmt.Errorf("没有可用的IP")
	}

	ip := ips[0]
	err = g.storage.AllocateIP(ctx, ip, description)
	if err != nil {
		return "", err
	}

	return ip, nil
}

// AllocateCIDR 从IP池中分配一个指定大小的CIDR
func (g *CIDRGuardian) AllocateCIDR(ctx context.Context, bits int, description string) (string, error) {
	// 1. 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return "", err
	}

	// 2. 验证位数参数
	if bits < 0 || bits > 32 {
		return "", fmt.Errorf("无效的子网掩码位数: %d", bits)
	}

	// 3. 获取所有可用IP
	availableIPs, err := g.storage.GetAvailableIPs(ctx)
	if err != nil {
		return "", err
	}

	// 4. 计算需要的IP数量
	size := 1 << (32 - bits)

	// 5. 检查是否有足够的IP
	if len(availableIPs) < size {
		return "", fmt.Errorf("没有足够的IP可以分配 /%d 子网", bits)
	}

	// 6. 对IP进行排序以确保一致性
	sort.Strings(availableIPs)

	// 7. 查找网络对齐的起始IP
	var startIP string
	var candidateStartIPs []string

	for _, ipStr := range availableIPs {
		ip := net.ParseIP(ipStr)
		if ip == nil || ip.To4() == nil {
			continue
		}

		// 检查IP是否网络对齐
		mask := net.CIDRMask(bits, 32)
		maskedIP := cloneIP(ip.Mask(mask))
		if maskedIP.String() == ipStr {
			candidateStartIPs = append(candidateStartIPs, ipStr)
		}
	}

	// 8. 按照字典顺序选择第一个候选起始IP
	if len(candidateStartIPs) > 0 {
		sort.Strings(candidateStartIPs)
		startIP = candidateStartIPs[0]
	} else {
		return "", fmt.Errorf("没有找到网络对齐的起始IP")
	}

	// 9. 创建CIDR
	cidr := fmt.Sprintf("%s/%d", startIP, bits)
	_, ipNet, _ := net.ParseCIDR(cidr)

	// 10. 检查子网中的所有IP是否可用
	ipCount := 0
	for ip := cloneIP(ipNet.IP); ipNet.Contains(ip) && ipCount < size; nextIP(ip) {
		// 这里显式调用IsIPAvailable以保持与测试的兼容性
		available, err := g.storage.IsIPAvailable(ctx, ip.String())
		if err != nil {
			return "", err
		}
		if !available {
			return "", fmt.Errorf("IP %s 不可用", ip.String())
		}
		ipCount++
	}

	// 11. 首先标记网络地址为已分配
	if err := g.storage.AllocateIP(ctx, startIP, fmt.Sprintf("%s - %s", cidr, description)); err != nil {
		return "", err
	}

	// 12. 从可用池中移除其他IP (不包括已分配的网络地址)
	ipCount = 0
	for ip := cloneIP(ipNet.IP); ipNet.Contains(ip) && ipCount < size; nextIP(ip) {
		ipStr := ip.String()
		if ipStr != startIP { // 跳过已分配的网络地址
			if err := g.storage.RemoveIP(ctx, ipStr); err != nil {
				// 发生错误时回滚
				g.storage.DeallocateIP(ctx, startIP) // 尝试回滚网络地址的分配
				return "", err
			}
		}
		ipCount++
	}

	// 13. 返回CIDR
	return cidr, nil
}

// ReleaseIP 释放一个已分配的IP
func (g *CIDRGuardian) ReleaseIP(ctx context.Context, ipStr string) error {
	return g.storage.DeallocateIP(ctx, ipStr)
}

// ReleaseCIDR 释放一个已分配的CIDR
func (g *CIDRGuardian) ReleaseCIDR(ctx context.Context, cidr string) error {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return err
	}

	// 解析CIDR
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("无效的CIDR格式: %v", err)
	}

	// 检查网络地址是否已被分配
	networkAddr := ipNet.IP.Mask(ipNet.Mask).String()
	allocated, err := g.storage.GetAllocatedIPs(ctx)
	if err != nil {
		return err
	}

	if _, exists := allocated[networkAddr]; !exists {
		return fmt.Errorf("CIDR %s 未被分配", cidr)
	}

	// 将IP重新添加到可用池中
	for ip := cloneIP(ipNet.IP.Mask(ipNet.Mask)); ipNet.Contains(ip); nextIP(ip) {
		// 检查上下文是否已取消
		if err := ctx.Err(); err != nil {
			return err
		}

		// 检查IP是否在任何管理的 CIDR 范围内
		ipStr := ip.String()
		inManagedRange := false

		g.mu.RLock()
		for _, cidrInfo := range g.managedCIDRs {
			if cidrInfo.IPNet.Contains(ip) {
				inManagedRange = true
				break
			}
		}
		g.mu.RUnlock()

		if inManagedRange {
			// 只有当IP不在已分配列表中时，才添加到可用池
			if _, exists := allocated[ipStr]; !exists {
				if err := g.storage.AddIP(ctx, ipStr); err != nil {
					// 忽略"IP已存在"错误
					if !strings.Contains(err.Error(), "已被分配") && !strings.Contains(err.Error(), "already allocated") {
						return err
					}
				}
			}
		}
	}

	// 从已用CIDR中移除网络地址
	if err := g.storage.DeallocateIP(ctx, networkAddr); err != nil {
		return err
	}

	return nil
}

// GetAvailableCIDRs 获取当前可用的CIDR块
func (g *CIDRGuardian) GetAvailableCIDRs(ctx context.Context) ([]string, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// 获取所有可用的IP
	availableIPs, err := g.storage.GetAvailableIPs(ctx)
	if err != nil {
		return nil, err
	}

	if len(availableIPs) == 0 {
		return nil, nil
	}

	// 简化实现，将可用IP按照/24网段分组
	cidrs := make(map[string]bool)

	for _, ipStr := range availableIPs {
		// 检查上下文是否已取消
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}

		// 创建/24网段
		mask := net.CIDRMask(24, 32)
		network := ip.Mask(mask)
		cidr := fmt.Sprintf("%s/24", network.String())
		cidrs[cidr] = true
	}

	result := make([]string, 0, len(cidrs))
	for cidr := range cidrs {
		result = append(result, cidr)
	}

	sort.Strings(result)
	return result, nil
}

// GetUsedCIDRs 获取已分配的CIDR及其描述
func (g *CIDRGuardian) GetUsedCIDRs(ctx context.Context) (map[string]string, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// 从已分配的IP中提取CIDR信息
	allocated, err := g.storage.GetAllocatedIPs(ctx)
	if err != nil {
		return nil, err
	}

	result := make(map[string]string)

	for _, desc := range allocated {
		// 检查上下文是否已取消
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		if strings.Contains(desc, " - ") {
			parts := strings.SplitN(desc, " - ", 2)
			cidr, description := parts[0], parts[1]
			result[cidr] = description
		}
	}

	return result, nil
}

// AvailableCount 返回可用IP数量
func (g *CIDRGuardian) AvailableCount(ctx context.Context) (int, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	return g.storage.AvailableCount(ctx)
}

// AllocatedCount 返回已分配IP数量
func (g *CIDRGuardian) AllocatedCount(ctx context.Context) (int, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	return g.storage.AllocatedCount(ctx)
}

// String 返回IP池的字符串表示
func (g *CIDRGuardian) String(ctx context.Context) (string, error) {
	var sb strings.Builder

	// 获取所有管理的CIDR
	managedCIDRs, err := g.GetManagedCIDRs(ctx)
	if err != nil {
		return "", err
	}

	sb.WriteString("CIDRGuardian 状态\n")
	sb.WriteString("管理的CIDR:\n")
	if len(managedCIDRs) == 0 {
		sb.WriteString("  无\n")
	} else {
		for cidr, desc := range managedCIDRs {
			sb.WriteString(fmt.Sprintf("  %s - %s\n", cidr, desc))
		}
	}

	// 已分配的CIDR
	sb.WriteString("\n已分配的CIDR:\n")
	usedCIDRs, err := g.GetUsedCIDRs(ctx)
	if err != nil {
		return "", err
	}

	if len(usedCIDRs) == 0 {
		sb.WriteString("  无\n")
	} else {
		for cidr, desc := range usedCIDRs {
			sb.WriteString(fmt.Sprintf("  %s - %s\n", cidr, desc))
		}
	}

	// IP 统计
	availCount, err := g.AvailableCount(ctx)
	if err != nil {
		return "", err
	}
	allocCount, err := g.AllocatedCount(ctx)
	if err != nil {
		return "", err
	}

	sb.WriteString(fmt.Sprintf("\nIP统计:\n  可用IP数量: %d\n  已分配IP数量: %d\n", availCount, allocCount))

	// 可用CIDR概览
	availableCIDRs, err := g.GetAvailableCIDRs(ctx)
	if err != nil {
		return "", err
	}

	sb.WriteString("\n可用CIDR概览:\n")
	if len(availableCIDRs) == 0 {
		sb.WriteString("  无\n")
	} else {
		for _, cidr := range availableCIDRs {
			sb.WriteString(fmt.Sprintf("  %s\n", cidr))
		}
	}

	return sb.String(), nil
}
