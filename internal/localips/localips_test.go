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

// 缓存命中：TTL 内重复调用应返回同一份底层数组（不重复探测网卡）。
func TestGetCachesWithinTTL(t *testing.T) {
	mu.Lock()
	cached = nil // 清掉前一个测试可能留下的缓存，确保首次 Get 真正探测
	mu.Unlock()

	first := Get()
	second := Get()
	if len(first) != len(second) {
		t.Fatalf("缓存前后长度不同: %v vs %v", first, second)
	}
	if &first[0] != &second[0] {
		t.Fatal("TTL 内两次 Get 未复用缓存（返回了不同底层数组）")
	}
}
