package localips

import (
	"net"
	"testing"
)

func ipNet(cidr string) *net.IPNet {
	ip, n, err := net.ParseCIDR(cidr)
	if err != nil {
		panic(err)
	}
	n.IP = ip
	return n
}

// fakeAddr 满足 net.Addr 但不是 *net.IPNet，必须被跳过。
type fakeAddr struct{}

func (fakeAddr) Network() string { return "fake" }
func (fakeAddr) String() string  { return "fake" }

func TestFilterAddrsKeepsGlobalIPv4(t *testing.T) {
	got := filterAddrs([]net.Addr{
		ipNet("192.168.1.20/24"),
		ipNet("10.0.0.5/8"),
		ipNet("172.16.3.4/12"),
	})
	want := []string{"192.168.1.20", "10.0.0.5", "172.16.3.4"}
	if len(got) != len(want) {
		t.Fatalf("filterAddrs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("filterAddrs = %v, want %v", got, want)
		}
	}
}

func TestFilterAddrsDropsLoopbackLinkLocalMulticast(t *testing.T) {
	got := filterAddrs([]net.Addr{
		ipNet("127.0.0.1/8"),      // 回环
		ipNet("169.254.10.20/16"), // 链路本地
		ipNet("224.0.0.1/4"),      // 多播
		ipNet("239.1.2.3/4"),      // 多播（管理范围）
		ipNet("192.168.1.20/24"),  // 唯一可用的
	})
	if len(got) != 1 || got[0] != "192.168.1.20" {
		t.Fatalf("filterAddrs = %v, want [192.168.1.20]", got)
	}
}

// 回环被排除，但"完全没有可用地址"时必须回退到 127.0.0.1：
// 返回空切片会让广播出去的地址列表为空，用户拿不到可访问链接。
func TestFilterAddrsFallbackToLoopback(t *testing.T) {
	for _, addrs := range [][]net.Addr{
		nil,
		{},
		{ipNet("127.0.0.1/8")},
		{ipNet("169.254.1.1/16")},
		{fakeAddr{}},
	} {
		got := filterAddrs(addrs)
		if len(got) != 1 || got[0] != "127.0.0.1" {
			t.Errorf("filterAddrs(%v) = %v, want [127.0.0.1]", addrs, got)
		}
	}
}

func TestFilterAddrsSkipsNonIPNet(t *testing.T) {
	got := filterAddrs([]net.Addr{fakeAddr{}, ipNet("192.168.1.20/24"), fakeAddr{}})
	if len(got) != 1 || got[0] != "192.168.1.20" {
		t.Fatalf("filterAddrs = %v, want [192.168.1.20]", got)
	}
}

// IPv6 地址经 To4() 过滤掉（只广播 IPv4，前端链接按 192.168.x.x 拼接）。
func TestFilterAddrsDropsIPv6(t *testing.T) {
	got := filterAddrs([]net.Addr{
		ipNet("2001:db8::1/32"),
		ipNet("fe80::1/64"), // 链路本地 v6，同时被 IsLinkLocalUnicast 命中
	})
	if len(got) != 1 || got[0] != "127.0.0.1" {
		t.Fatalf("filterAddrs = %v, want 回退 [127.0.0.1]", got)
	}

	// IPv4-mapped IPv6（::ffff:192.168.1.20）应还原为点分四段 IPv4。
	got = filterAddrs([]net.Addr{ipNet("::ffff:192.168.1.20/128")})
	if len(got) != 1 || got[0] != "192.168.1.20" {
		t.Fatalf("filterAddrs(IPv4-mapped) = %v, want [192.168.1.20]", got)
	}
}

// Get 必须永不返回 nil/空（handleServerMessage 直接把它拼进广播消息）。
func TestGetNeverEmpty(t *testing.T) {
	got := Get()
	if len(got) == 0 {
		t.Fatal("Get() 返回空切片")
	}
	for _, s := range got {
		if ip := net.ParseIP(s); ip == nil {
			t.Errorf("Get() 返回了非法 IP: %q", s)
		} else if ip.To4() == nil {
			t.Errorf("Get() 返回了非 IPv4: %q", s)
		} else if ip.IsLoopback() && len(got) > 1 {
			t.Errorf("Get() 在有其他地址时仍包含回环: %v", got)
		}
	}
}

// 缓存命中：TTL 内重复调用不应重复探测网卡（返回内容一致）。
//
// 注意**不能**断言两次返回同一个底层数组：Get 必须返回副本，否则调用方
// （state.shareInfo.IPs / share.Server 的 Host 白名单）改动返回值会污染缓存。
func TestGetCachesWithinTTL(t *testing.T) {
	mu.Lock()
	cached = nil // 清掉前一个测试可能留下的缓存，确保首次 Get 真正探测
	mu.Unlock()

	first := Get()
	second := Get()
	if len(first) != len(second) {
		t.Fatalf("缓存前后长度不同: %v vs %v", first, second)
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("缓存前后内容不同: %v vs %v", first, second)
		}
	}
}

// Get 必须返回副本：调用方会把它存进 share_info 并冻结成 Host 白名单，
// 若返回缓存本体，改动返回值就会悄悄改写那份白名单。
func TestGetReturnsCopy(t *testing.T) {
	mu.Lock()
	cached = nil
	mu.Unlock()

	first := Get()
	if len(first) == 0 {
		t.Fatal("前置条件：应至少有一个地址")
	}
	original := first[0]

	// 篡改返回值
	first[0] = "203.0.113.7"

	second := Get()
	if second[0] != original {
		t.Fatalf("篡改返回值污染了缓存：第二次拿到 %q，期望 %q", second[0], original)
	}

	// 缓存本体也不该把篡改写进去
	mu.Lock()
	cachedFirst := cached[0]
	mu.Unlock()
	if cachedFirst != original {
		t.Fatalf("包内缓存被调用方改写：%q，期望 %q", cachedFirst, original)
	}
}
