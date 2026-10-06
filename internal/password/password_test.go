package password

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	m, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}
	t.Cleanup(m.Close)
	return m
}

func TestNewGeneratesAndPersistsPepper(t *testing.T) {
	dir := t.TempDir()
	m, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if len(m.pepper) != pepperHexLength {
		t.Fatalf("pepper 长度应为 %d: %q", pepperHexLength, m.pepper)
	}
	data, err := os.ReadFile(filepath.Join(dir, "password_config.json"))
	if err != nil {
		t.Fatalf("配置应立即落盘: %v", err)
	}
	var cfg config
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Pepper == nil || *cfg.Pepper != m.pepper {
		t.Fatalf("落盘的 pepper 应一致: %+v", cfg)
	}
	if cfg.Enabled {
		t.Fatal("初始应禁用保护")
	}
}

func TestConfigSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	m1, _ := New(dir)
	m1.SetPassword("1234")
	m1.SetPasswordEnabled(true)
	m1.Close()

	m2, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer m2.Close()
	status := m2.Status()
	if !status.Enabled || !status.HasPassword {
		t.Fatalf("重启后应保留配置: %+v", status)
	}
	if m2.pepper != m1.pepper {
		t.Fatal("重启后 pepper 不应变化")
	}
	// 旧 session 必须清空（重启后应重新登录）
	token, err := m2.Authenticate("1.2.3.4", "1234")
	if err != nil {
		t.Fatal(err)
	}
	if !m2.CheckWebAuth("session_token=" + token) {
		t.Fatal("新登录的 session 应有效")
	}
}

func TestCorruptConfigResets(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "password_config.json"), []byte("{broken"), 0o600)
	m, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	status := m.Status()
	if status.Enabled || status.HasPassword {
		t.Fatalf("损坏配置应重置为默认: %+v", status)
	}
	if m.pepper == "" {
		t.Fatal("重置后应生成新 pepper")
	}
}

func TestSetPasswordValidation(t *testing.T) {
	m := newTestManager(t)
	cases := []struct {
		pw, wantMsg string
	}{
		{"123", "密码必须是4位数字"},
		{"12345", "密码必须是4位数字"},
		{"abcd", "密码只能包含数字0-9"},
		{"12.4", "密码只能包含数字0-9"},
	}
	for _, c := range cases {
		err := m.SetPassword(c.pw)
		if err == nil || err.Error() != c.wantMsg {
			t.Fatalf("SetPassword(%q) 应报 %q, 实际 %v", c.pw, c.wantMsg, err)
		}
	}
	if err := m.SetPassword("0000"); err != nil {
		t.Fatalf("0000 应合法: %v", err)
	}
	if m.Status().HasPassword != true {
		t.Fatal("设置后应有密码")
	}
}

func TestSetPasswordClearsSessions(t *testing.T) {
	m := newTestManager(t)
	m.SetPassword("1111")
	token, err := m.Authenticate("1.1.1.1", "1111")
	if err != nil {
		t.Fatal(err)
	}
	m.SetPassword("2222")
	if m.CheckWebAuth("session_token=" + token) {
		t.Fatal("改密后旧 session 必须失效")
	}
	if _, err := m.Authenticate("1.1.1.1", "1111"); err == nil {
		t.Fatal("旧密码不应通过")
	}
	if _, err := m.Authenticate("1.1.1.1", "2222"); err != nil {
		t.Fatalf("新密码应通过: %v", err)
	}
}

func TestSetPasswordEnabledRequiresPassword(t *testing.T) {
	m := newTestManager(t)
	err := m.SetPasswordEnabled(true)
	if err == nil || err.Error() != "请先设置密码再启用密码保护" {
		t.Fatalf("未设密码时启用应被拒: %v", err)
	}
	m.SetPassword("9999")
	if err := m.SetPasswordEnabled(true); err != nil {
		t.Fatal(err)
	}
	if !m.Enabled() {
		t.Fatal("应已启用")
	}
	m.ResetPassword()
	if m.Enabled() || m.Status().HasPassword {
		t.Fatal("ResetPassword 应清密码并禁用保护")
	}
}

func TestAuthenticateWrongPassword(t *testing.T) {
	m := newTestManager(t)
	m.SetPassword("1234")
	ip := "9.9.9.9"
	_, err := m.Authenticate(ip, "0000")
	if err == nil || err.Error() != "密码错误，请重试" {
		t.Fatalf("错误密码提示不符: %v", err)
	}
	// 第二次失败仍未达阈值，应仍是普通错误而非锁定
	_, err = m.Authenticate(ip, "0000")
	if err == nil || err.Error() != "密码错误，请重试" {
		t.Fatalf("第二次失败不应锁定: %v", err)
	}
}

func TestRateLimitLocksAfterThreeFailures(t *testing.T) {
	m := newTestManager(t)
	m.SetPassword("1234")
	ip := "10.0.0.1"
	for i := 0; i < 3; i++ {
		if _, err := m.Authenticate(ip, "0000"); err == nil {
			t.Fatalf("第 %d 次不应成功", i+1)
		}
	}
	_, err := m.Authenticate(ip, "1234")
	if err == nil || !strings.Contains(err.Error(), "访问已锁定，请") {
		t.Fatalf("3 次失败后即使密码正确也应锁定: %v", err)
	}
	// 锁定文案必须包含剩余秒数（登录页靠正则提取做倒计时）
	if !strings.Contains(err.Error(), "秒后重试") {
		t.Fatalf("锁定文案缺秒数: %v", err)
	}
	// 其他 IP 不受影响
	if token, err := m.Authenticate("10.0.0.2", "1234"); err != nil || token == "" {
		t.Fatalf("其他 IP 不应被牵连: %v", err)
	}
}

func TestRateLimitResetsAfterLockExpiry(t *testing.T) {
	m := newTestManager(t)
	m.SetPassword("1234")
	ip := "10.0.0.1"
	for i := 0; i < 3; i++ {
		m.Authenticate(ip, "0000")
	}
	// 白盒：把锁定时刻拨回过去，模拟锁到期
	m.mu.Lock()
	m.failed[ip].lockedUntil = time.Now().Add(-time.Second)
	m.mu.Unlock()

	if _, err := m.Authenticate(ip, "1234"); err != nil {
		t.Fatalf("锁到期后应重新获得机会: %v", err)
	}
	m.mu.Lock()
	_, still := m.failed[ip]
	m.mu.Unlock()
	if still {
		t.Fatal("成功登录后应清除失败记录")
	}
}

func TestRateLimitFailureWindowPrune(t *testing.T) {
	m := newTestManager(t)
	m.SetPassword("1234")
	ip := "10.0.0.1"
	m.Authenticate(ip, "0000")
	m.Authenticate(ip, "0000")
	// 两次失败（未达 3 次），把 lastFailed 拨回 30 秒前 → 记录应被清理
	m.mu.Lock()
	m.failed[ip].lastFailed = time.Now().Add(-31 * time.Second)
	m.mu.Unlock()
	m.Authenticate(ip, "0000") // 触发一次清理+记录
	m.mu.Lock()
	a := m.failed[ip]
	m.mu.Unlock()
	if a == nil || a.count != 1 {
		t.Fatalf("过期窗口外的记录应被清零重计: %+v", a)
	}
}

func TestSessionExpiry(t *testing.T) {
	m := newTestManager(t)
	m.SetPassword("1234")
	token, err := m.Authenticate("1.1.1.1", "1234")
	if err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	m.sessions[token] = time.Now().Add(-time.Second)
	m.mu.Unlock()
	if m.CheckWebAuth("session_token=" + token) {
		t.Fatal("过期 session 应无效")
	}
	if m.CheckWebAuth("") || m.CheckWebAuth("other=1") {
		t.Fatal("无 token 应无效")
	}
}

func TestHashAndVerify(t *testing.T) {
	h := hashPassword("1234", "pepper")
	if !strings.HasPrefix(h, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("PHC 格式不符: %q", h)
	}
	if !verifyPassword(h, "1234", "pepper") {
		t.Fatal("正确密码应通过验证")
	}
	if verifyPassword(h, "1235", "pepper") {
		t.Fatal("错误密码不应通过")
	}
	if verifyPassword(h, "1234", "other") {
		t.Fatal("pepper 不同不应通过")
	}
	if verifyPassword("garbage", "1234", "pepper") {
		t.Fatal("畸形 PHC 应返回失败而非 panic")
	}
	// 两次哈希 salt 不同，PHC 串必须不同
	if h == hashPassword("1234", "pepper") {
		t.Fatal("随机 salt 应产生不同哈希")
	}
}

func TestGenerateRandomPassword(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		pw := GenerateRandomPassword()
		if len(pw) != 4 || !isAllDigits(pw) {
			t.Fatalf("随机密码格式不符: %q", pw)
		}
		seen[pw] = true
	}
	if len(seen) < 2 {
		t.Fatal("随机密码不应总是相同值")
	}
}

func isAllDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
