package CIDRGuardian

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql" // MySQL 驱动
	_ "github.com/lib/pq"              // PostgreSQL 驱动
)

// SQLIPStorage 是 IP 池存储的 SQL 实现
type SQLIPStorage struct {
	db         *sql.DB
	driverName string
}

// SQLConfig 存储 SQL 连接配置
type SQLConfig struct {
	DriverName      string
	DataSourceName  string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

// NewSQLIPStorage 创建一个新的 SQL IP 存储
func NewSQLIPStorage(ctx context.Context, config SQLConfig) (*SQLIPStorage, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// 验证驱动名称
	if config.DriverName != "mysql" && config.DriverName != "postgres" {
		return nil, fmt.Errorf("不支持的数据库驱动: %s (支持: mysql, postgres)", config.DriverName)
	}

	// 连接数据库
	db, err := sql.Open(config.DriverName, config.DataSourceName)
	if err != nil {
		return nil, fmt.Errorf("连接数据库失败: %v", err)
	}

	// 设置连接池参数
	if config.MaxOpenConns > 0 {
		db.SetMaxOpenConns(config.MaxOpenConns)
	}
	if config.MaxIdleConns > 0 {
		db.SetMaxIdleConns(config.MaxIdleConns)
	}
	if config.ConnMaxLifetime > 0 {
		db.SetConnMaxLifetime(config.ConnMaxLifetime)
	}
	if config.ConnMaxIdleTime > 0 {
		db.SetConnMaxIdleTime(config.ConnMaxIdleTime)
	}

	// 检查连接是否有效
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("数据库连接测试失败: %v", err)
	}

	// 创建存储实例
	storage := &SQLIPStorage{
		db:         db,
		driverName: config.DriverName,
	}

	// 初始化必要的表
	if err := storage.initTables(ctx); err != nil {
		db.Close()
		return nil, err
	}

	return storage, nil
}

// initTables 创建必要的数据库表
func (s *SQLIPStorage) initTables(ctx context.Context) error {
	var createAvailableTableSQL, createAllocatedTableSQL string

	if s.driverName == "mysql" {
		createAvailableTableSQL = `
		CREATE TABLE IF NOT EXISTS ip_available (
			ip VARCHAR(45) PRIMARY KEY,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		) ENGINE=InnoDB;`

		createAllocatedTableSQL = `
		CREATE TABLE IF NOT EXISTS ip_allocated (
			ip VARCHAR(45) PRIMARY KEY,
			description TEXT,
			allocated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		) ENGINE=InnoDB;`
	} else if s.driverName == "postgres" {
		createAvailableTableSQL = `
		CREATE TABLE IF NOT EXISTS ip_available (
			ip VARCHAR(45) PRIMARY KEY,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);`

		createAllocatedTableSQL = `
		CREATE TABLE IF NOT EXISTS ip_allocated (
			ip VARCHAR(45) PRIMARY KEY,
			description TEXT,
			allocated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);`
	}

	// 创建可用 IP 表
	if _, err := s.db.ExecContext(ctx, createAvailableTableSQL); err != nil {
		return fmt.Errorf("创建 ip_available 表失败: %v", err)
	}

	// 创建已分配 IP 表
	if _, err := s.db.ExecContext(ctx, createAllocatedTableSQL); err != nil {
		return fmt.Errorf("创建 ip_allocated 表失败: %v", err)
	}

	return nil
}

// Close 关闭数据库连接
func (s *SQLIPStorage) Close() error {
	return s.db.Close()
}

// AddIP 实现 IPStorage 接口
func (s *SQLIPStorage) AddIP(ctx context.Context, ip string) error {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return err
	}

	// 开始事务
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始事务失败: %v", err)
	}
	defer tx.Rollback()

	// 检查 IP 是否已分配
	var count int
	var checkAllocatedSQL string

	if s.driverName == "mysql" {
		checkAllocatedSQL = "SELECT COUNT(*) FROM ip_allocated WHERE ip = ?"
	} else {
		checkAllocatedSQL = "SELECT COUNT(*) FROM ip_allocated WHERE ip = $1"
	}

	if err := tx.QueryRowContext(ctx, checkAllocatedSQL, ip).Scan(&count); err != nil {
		return fmt.Errorf("检查 IP 是否已分配失败: %v", err)
	}

	if count > 0 {
		return fmt.Errorf("IP %s 已被分配", ip)
	}

	// 添加到可用池
	var insertSQL string
	if s.driverName == "mysql" {
		insertSQL = "INSERT INTO ip_available (ip) VALUES (?) ON DUPLICATE KEY UPDATE ip = ip"
	} else {
		insertSQL = "INSERT INTO ip_available (ip) VALUES ($1) ON CONFLICT (ip) DO NOTHING"
	}

	if _, err := tx.ExecContext(ctx, insertSQL, ip); err != nil {
		return fmt.Errorf("添加 IP 到可用池失败: %v", err)
	}

	// 提交事务
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务失败: %v", err)
	}

	return nil
}

// RemoveIP 实现 IPStorage 接口
func (s *SQLIPStorage) RemoveIP(ctx context.Context, ip string) error {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return err
	}

	// 开始事务
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始事务失败: %v", err)
	}
	defer tx.Rollback()

	// 检查 IP 是否可用
	var count int
	var checkAvailableSQL string

	if s.driverName == "mysql" {
		checkAvailableSQL = "SELECT COUNT(*) FROM ip_available WHERE ip = ?"
	} else {
		checkAvailableSQL = "SELECT COUNT(*) FROM ip_available WHERE ip = $1"
	}

	if err := tx.QueryRowContext(ctx, checkAvailableSQL, ip).Scan(&count); err != nil {
		return fmt.Errorf("检查 IP 是否可用失败: %v", err)
	}

	if count == 0 {
		return fmt.Errorf("IP %s 不在可用池中", ip)
	}

	// 从可用池中移除
	var deleteSQL string
	if s.driverName == "mysql" {
		deleteSQL = "DELETE FROM ip_available WHERE ip = ?"
	} else {
		deleteSQL = "DELETE FROM ip_available WHERE ip = $1"
	}

	if _, err := tx.ExecContext(ctx, deleteSQL, ip); err != nil {
		return fmt.Errorf("从可用池中移除 IP 失败: %v", err)
	}

	// 提交事务
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务失败: %v", err)
	}

	return nil
}

// IsIPAvailable 实现 IPStorage 接口
func (s *SQLIPStorage) IsIPAvailable(ctx context.Context, ip string) (bool, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return false, err
	}

	var count int
	var query string

	if s.driverName == "mysql" {
		query = "SELECT COUNT(*) FROM ip_available WHERE ip = ?"
	} else {
		query = "SELECT COUNT(*) FROM ip_available WHERE ip = $1"
	}

	if err := s.db.QueryRowContext(ctx, query, ip).Scan(&count); err != nil {
		return false, fmt.Errorf("检查 IP 可用性失败: %v", err)
	}

	return count > 0, nil
}

// GetAvailableIPs 实现 IPStorage 接口
func (s *SQLIPStorage) GetAvailableIPs(ctx context.Context) ([]string, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	query := "SELECT ip FROM ip_available ORDER BY ip"
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("获取可用 IP 列表失败: %v", err)
	}
	defer rows.Close()

	var ips []string
	for rows.Next() {
		var ip string
		if err := rows.Scan(&ip); err != nil {
			return nil, fmt.Errorf("读取 IP 失败: %v", err)
		}
		ips = append(ips, ip)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("迭代结果集失败: %v", err)
	}

	return ips, nil
}

// AllocateIP 实现 IPStorage 接口
func (s *SQLIPStorage) AllocateIP(ctx context.Context, ip string, description string) error {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return err
	}

	// 开始事务
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始事务失败: %v", err)
	}
	defer tx.Rollback()

	// 检查 IP 是否可用
	var checkAvailableSQL string
	var count int

	if s.driverName == "mysql" {
		checkAvailableSQL = "SELECT COUNT(*) FROM ip_available WHERE ip = ?"
	} else {
		checkAvailableSQL = "SELECT COUNT(*) FROM ip_available WHERE ip = $1"
	}

	if err := tx.QueryRowContext(ctx, checkAvailableSQL, ip).Scan(&count); err != nil {
		return fmt.Errorf("检查 IP 是否可用失败: %v", err)
	}

	if count == 0 {
		return fmt.Errorf("IP %s 不在可用池中", ip)
	}

	// 从可用池中移除
	var deleteSQL string
	if s.driverName == "mysql" {
		deleteSQL = "DELETE FROM ip_available WHERE ip = ?"
	} else {
		deleteSQL = "DELETE FROM ip_available WHERE ip = $1"
	}

	if _, err := tx.ExecContext(ctx, deleteSQL, ip); err != nil {
		return fmt.Errorf("从可用池中移除 IP 失败: %v", err)
	}

	// 添加到已分配池
	var insertSQL string
	if s.driverName == "mysql" {
		insertSQL = "INSERT INTO ip_allocated (ip, description) VALUES (?, ?)"
	} else {
		insertSQL = "INSERT INTO ip_allocated (ip, description) VALUES ($1, $2)"
	}

	if _, err := tx.ExecContext(ctx, insertSQL, ip, description); err != nil {
		return fmt.Errorf("添加 IP 到已分配池失败: %v", err)
	}

	// 提交事务
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务失败: %v", err)
	}

	return nil
}

// DeallocateIP 实现 IPStorage 接口
func (s *SQLIPStorage) DeallocateIP(ctx context.Context, ip string) error {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return err
	}

	// 开始事务
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始事务失败: %v", err)
	}
	defer tx.Rollback()

	// 检查 IP 是否已分配
	var checkAllocatedSQL string
	var count int

	if s.driverName == "mysql" {
		checkAllocatedSQL = "SELECT COUNT(*) FROM ip_allocated WHERE ip = ?"
	} else {
		checkAllocatedSQL = "SELECT COUNT(*) FROM ip_allocated WHERE ip = $1"
	}

	if err := tx.QueryRowContext(ctx, checkAllocatedSQL, ip).Scan(&count); err != nil {
		return fmt.Errorf("检查 IP 是否已分配失败: %v", err)
	}

	if count == 0 {
		return fmt.Errorf("IP %s 不在已分配池中", ip)
	}

	// 从已分配池中移除
	var deleteSQL string
	if s.driverName == "mysql" {
		deleteSQL = "DELETE FROM ip_allocated WHERE ip = ?"
	} else {
		deleteSQL = "DELETE FROM ip_allocated WHERE ip = $1"
	}

	if _, err := tx.ExecContext(ctx, deleteSQL, ip); err != nil {
		return fmt.Errorf("从已分配池中移除 IP 失败: %v", err)
	}

	// 添加到可用池
	var insertSQL string
	if s.driverName == "mysql" {
		insertSQL = "INSERT INTO ip_available (ip) VALUES (?)"
	} else {
		insertSQL = "INSERT INTO ip_available (ip) VALUES ($1)"
	}

	if _, err := tx.ExecContext(ctx, insertSQL, ip); err != nil {
		return fmt.Errorf("添加 IP 到可用池失败: %v", err)
	}

	// 提交事务
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务失败: %v", err)
	}

	return nil
}

// GetAllocatedIPs 实现 IPStorage 接口
func (s *SQLIPStorage) GetAllocatedIPs(ctx context.Context) (map[string]string, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	query := "SELECT ip, description FROM ip_allocated"
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("获取已分配 IP 列表失败: %v", err)
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var ip, desc string
		if err := rows.Scan(&ip, &desc); err != nil {
			return nil, fmt.Errorf("读取 IP 和描述失败: %v", err)
		}
		result[ip] = desc
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("迭代结果集失败: %v", err)
	}

	return result, nil
}

// AvailableCount 实现 IPStorage 接口
func (s *SQLIPStorage) AvailableCount(ctx context.Context) (int, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	var count int
	query := "SELECT COUNT(*) FROM ip_available"
	if err := s.db.QueryRowContext(ctx, query).Scan(&count); err != nil {
		return 0, fmt.Errorf("获取可用 IP 数量失败: %v", err)
	}

	return count, nil
}

// AllocatedCount 实现 IPStorage 接口
func (s *SQLIPStorage) AllocatedCount(ctx context.Context) (int, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	var count int
	query := "SELECT COUNT(*) FROM ip_allocated"
	if err := s.db.QueryRowContext(ctx, query).Scan(&count); err != nil {
		return 0, fmt.Errorf("获取已分配 IP 数量失败: %v", err)
	}

	return count, nil
}
