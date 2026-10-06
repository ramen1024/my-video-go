// Package password 实现局域网共享的访问密码保护。
//
// 组成：Argon2id 哈希（4 位数字密码 + 随机 pepper）、session（内存 map +
// 过期时间）、按 IP 登录限流、配置持久化（password_config.json，0600）。
//
// 威胁模型：4 位数字只有 10000 种组合，靠 Argon2id 拖慢单次尝试 +
// 每 30 秒窗口内最多 3 次失败的 IP 限流，把穷举时间拉长到数天量级。
package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"my-video-go/internal/apperr"
	"my-video-go/internal/constants"
	"my-video-go/internal/models"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
)

// Argon2id 参数，与原 Tauri 版 argon2 crate 默认值一致（PHC 串
// $argon2id$v=19$m=19456,t=2,p=1$...）。
const (
	argonTimeCost   = 2
	argonMemoryKiB  = 19456
	argonThreads    = 1
	argonKeyLen     = 32
	argonSaltLen    = 16
	pepperHexLength = 64 // 32 随机字节的 hex
)

type failedAttempt struct {
	count       int
	lockedUntil time.Time // 零值表示未锁定
	lastFailed  time.Time
}

type config struct {
	PasswordHash *string `json:"password_hash"`
	Enabled      bool    `json:"enabled"`
	Pepper       *string `json:"pepper"`
}

// Manager 持有密码保护的全部可变状态，方法并发安全。
type Manager struct {
	mu       sync.Mutex
	hash     string
	enabled  bool
	pepper   string
	sessions map[string]time.Time      // token → 过期时刻
	failed   map[string]*failedAttempt // ip → 失败记录
	dir      string                    // 配置目录

	stopCleanup chan struct{}
}

// New 从 dir/password_config.json 加载配置（文件不存在或损坏时回退默认值）；
// pepper 缺失时生成新的随机 pepper 并立即持久化。
func New(dir string) (*Manager, error) {
	m := &Manager{
		sessions:    make(map[string]time.Time),
		failed:      make(map[string]*failedAttempt),
		dir:         dir,
		stopCleanup: make(chan struct{}),
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建配置目录失败: %w", err)
	}
	m.loadConfig()
	if m.pepper == "" {
		m.pepper = generatePepper()
		m.saveConfigLocked() // 首次生成必须立刻落盘，否则重启后哈希将无法验证
	}
	go m.cleanupLoop()
	return m, nil
}

// Close 停掉后台清理线程（进程退出前调用）。
func (m *Manager) Close() {
	close(m.stopCleanup)
}

func (m *Manager) configPath() string {
	return filepath.Join(m.dir, "password_config.json")
}

func (m *Manager) loadConfig() {
	data, err := os.ReadFile(m.configPath())
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Println("读取密码配置失败，使用默认设置:", err)
		}
		return
	}
	var cfg config
	if err := json.Unmarshal(data, &cfg); err != nil {
		// 配置损坏时静默重置（与原版一致）：相当于清除密码并禁用保护
		fmt.Println("密码配置解析失败（文件可能损坏），已重置:", err)
		return
	}
	if cfg.PasswordHash != nil {
		m.hash = *cfg.PasswordHash
	}
	m.enabled = cfg.Enabled
	if cfg.Pepper != nil {
		m.pepper = *cfg.Pepper
	}
}

// saveConfigLocked 要求持有 m.mu。
func (m *Manager) saveConfigLocked() {
	cfg := config{}
	if m.hash != "" {
		h := m.hash
		cfg.PasswordHash = &h
	}
	cfg.Enabled = m.enabled
	if m.pepper != "" {
		p := m.pepper
		cfg.Pepper = &p
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		fmt.Println("密码配置序列化失败:", err)
		return
	}
	if err := os.WriteFile(m.configPath(), data, 0o600); err != nil {
		fmt.Println("密码配置保存失败:", err)
	}
}

// ---- 状态查询与设置 ----

func (m *Manager) Status() models.PasswordStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return models.PasswordStatus{Enabled: m.enabled, HasPassword: m.hash != ""}
}

func (m *Manager) Enabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.enabled
}

// SetPasswordEnabled 启用/禁用密码保护。启用前提是已设置密码。
func (m *Manager) SetPasswordEnabled(enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if enabled && m.hash == "" {
		return apperr.PasswordError("请先设置密码再启用密码保护")
	}
	m.enabled = enabled
	m.saveConfigLocked()
	return nil
}

// SetPassword 设置新密码（必须 4 位纯数字），并清空全部已有 session。
func (m *Manager) SetPassword(password string) error {
	if len(password) != 4 {
		return apperr.PasswordError("密码必须是4位数字")
	}
	for i := 0; i < len(password); i++ {
		if password[i] < '0' || password[i] > '9' {
			return apperr.PasswordError("密码只能包含数字0-9")
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hash = hashPassword(password, m.pepper)
	m.sessions = make(map[string]time.Time)
	m.saveConfigLocked()
	return nil
}

// ResetPassword 清除密码、禁用保护并清空 session。
func (m *Manager) ResetPassword() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hash = ""
	m.enabled = false
	m.sessions = make(map[string]time.Time)
	m.saveConfigLocked()
	return nil
}

// GenerateRandomPassword 生成随机 4 位数字密码（含前导零）。
func GenerateRandomPassword() string {
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "0000" // crypto/rand 失败极罕见；退化为固定值由用户手动修改
	}
	n := (uint16(b[0])<<8 | uint16(b[1])) % 10000
	return fmt.Sprintf("%04d", n)
}

// ---- 认证 ----

// Authenticate 处理一次网页端登录：限流检查 → 密码验证 → 记录/清理失败计数。
// 成功返回新建的 session token。
func (m *Manager) Authenticate(ip, password string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.cleanupExpiredSessionsLocked()

	if m.hash == "" {
		return "", apperr.PasswordError("未设置密码")
	}
	if remaining := m.checkRateLimitLocked(ip); remaining > 0 {
		return "", apperr.PasswordError(fmt.Sprintf("访问已锁定，请%d秒后重试", remaining))
	}
	if !verifyPassword(m.hash, password, m.pepper) {
		m.recordFailedAttemptLocked(ip)
		return "", apperr.PasswordError("密码错误，请重试")
	}
	delete(m.failed, ip)
	return m.createSessionLocked(), nil
}

// checkRateLimitLocked 返回剩余锁定秒数（0 表示未锁定）。要求持有 m.mu。
func (m *Manager) checkRateLimitLocked(ip string) int {
	a, ok := m.failed[ip]
	if !ok || a.lockedUntil.IsZero() {
		return 0
	}
	remaining := int(time.Until(a.lockedUntil).Seconds())
	if remaining < 0 {
		return 0
	}
	return remaining
}

// recordFailedAttemptLocked 记一次失败；达到 MaxFailedAttempts 时锁定
// LockDurationSecs 并把计数清零（锁到期后重新获得完整次数）。要求持有 m.mu。
func (m *Manager) recordFailedAttemptLocked(ip string) {
	a := m.failed[ip]
	if a == nil {
		a = &failedAttempt{}
		m.failed[ip] = a
	}
	a.count++
	a.lastFailed = time.Now()
	if a.count >= constants.MaxFailedAttempts {
		a.lockedUntil = time.Now().Add(constants.LockDurationSecs * time.Second)
		a.count = 0
	}
}

func (m *Manager) cleanupExpiredSessionsLocked() {
	now := time.Now()
	for token, expiry := range m.sessions {
		if expiry.Before(now) {
			delete(m.sessions, token)
		}
	}
	window := constants.LockDurationSecs * time.Second
	for ip, a := range m.failed {
		// 保留"锁定中"与"窗口内仍在累计"的记录，其余清除
		if !a.lockedUntil.After(now) && now.Sub(a.lastFailed) >= window {
			delete(m.failed, ip)
		}
	}
}

// CleanupOnce 主动清理一次过期 session 与失败记录（/auth 请求入口会顺带调用）。
func (m *Manager) CleanupOnce() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupExpiredSessionsLocked()
}

func (m *Manager) cleanupLoop() {
	ticker := time.NewTicker(constants.SessionCleanupIntervalSecs * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.stopCleanup:
			return
		case <-ticker.C:
			m.mu.Lock()
			m.cleanupExpiredSessionsLocked()
			m.mu.Unlock()
		}
	}
}

// ---- Session ----

// createSessionLocked 签发新 token。要求持有 m.mu。
func (m *Manager) createSessionLocked() string {
	token := make([]byte, 32)
	_, _ = rand.Read(token) // Go 1.24 起 crypto/rand.Read 保证不返回错误
	t := hex.EncodeToString(token)
	m.sessions[t] = time.Now().Add(constants.SessionDurationSecs * time.Second)
	return t
}

// CheckWebAuth 校验 Cookie 头中的 session token 是否有效。
func (m *Manager) CheckWebAuth(cookieHeader string) bool {
	token := extractSessionToken(cookieHeader)
	if token == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	expiry, ok := m.sessions[token]
	return ok && expiry.After(time.Now())
}

// SessionCookie 构造 Set-Cookie 值。局域网为纯 HTTP，无法启用 Secure 属性；
// 若未来引入 HTTPS，应在此追加 "; Secure"。
func SessionCookie(token string) string {
	return fmt.Sprintf("session_token=%s; Path=/; Max-Age=%d; HttpOnly; SameSite=Strict",
		token, constants.SessionDurationSecs)
}

// extractSessionToken 从 Cookie 头解析 session_token 的值。
func extractSessionToken(header string) string {
	for _, part := range strings.Split(header, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "session_token=") {
			return strings.TrimPrefix(part, "session_token=")
		}
	}
	return ""
}

// ---- Argon2id 哈希 ----

var b64 = base64.RawStdEncoding

func hashPassword(password, pepper string) string {
	salt := make([]byte, argonSaltLen)
	_, _ = rand.Read(salt) // Go 1.24 起 crypto/rand.Read 保证不返回错误
	key := argon2.IDKey([]byte(password+pepper), salt, argonTimeCost, argonMemoryKiB, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemoryKiB, argonTimeCost, argonThreads, b64.EncodeToString(salt), b64.EncodeToString(key))
}

// verifyPassword 解析 PHC 串并重算哈希做常数时间比较。
func verifyPassword(phc, password, pepper string) bool {
	parts := strings.Split(phc, "$")
	// 期望形如: ["", "argon2id", "v=19", "m=19456,t=2,p=1", salt, key]
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var mem, iters, parallel uint32
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &iters, &parallel); err != nil {
		return false
	}
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := b64.DecodeString(parts[5])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(password+pepper), salt, iters, mem, uint8(parallel), uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

func generatePepper() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b) // Go 1.24 起 crypto/rand.Read 保证不返回错误
	return hex.EncodeToString(b)
}
