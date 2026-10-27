// Package settings 持久化跨进程的用户偏好。
//
// 目前只有一项：「上次成功扫描的文件夹」。AppState 是纯内存的，进程退出即清空，
// 于是每次重开应用都是空状态 —— 用户得重新选目录、重扫几十秒。这个包就是补上
// 那一格记忆。
//
// 与 `password_config.json` 的取舍不同：密码配置损坏必须**留痕**（静默重置等于
// 悄悄关掉密码保护，是安全事件）；这里的文件损坏只意味着"少一次自动恢复"，
// 因此只记一条 Warn 并回到默认值，绝不阻断启动。
package settings

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// fileName 是设置文件的固定名字（与 password_config.json 同目录）。
const fileName = "settings.json"

// Data 是落盘的设置内容。字段用 snake_case：它和其余 JSON 一样是"可以被
// 用户打开看一眼"的文件，不是内部结构。
type Data struct {
	// LastFolder 是上次**扫描成功**的文件夹（绝对路径）。
	LastFolder string `json:"last_folder"`
}

// Store 是设置的读写入口，并发安全。
type Store struct {
	path string

	mu   sync.Mutex
	data Data
}

// New 读取 dir 下的设置文件；不存在或损坏时返回一个可用的空 Store。
//
// 刻意**不返回 error**：设置读不出来从来不是启动失败的理由。目录不可写会在
// 第一次 SetLastFolder 时才暴露，那时也只记日志（见 App.scanAndRemember）。
func New(dir string) *Store {
	s := &Store{path: filepath.Join(dir, fileName)}
	if err := s.load(); err != nil {
		slog.Warn("读取设置失败，本次以默认值启动", "path", s.path, "err", err)
	}
	return s
}

func (s *Store) load() error {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // 首次运行
		}
		return err
	}
	var data Data
	if err := json.Unmarshal(raw, &data); err != nil {
		return err
	}
	s.mu.Lock()
	s.data = data
	s.mu.Unlock()
	return nil
}

// LastFolder 返回上次成功扫描的文件夹；没记录过时为空串。
//
// 只返回**记录值**，不校验目录是否还存在：那会让"U 盘拔了"表现为"设置被清了"。
// 插回去还能用，路径不可用的判断交给真正去扫描的一方。
func (s *Store) LastFolder() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.LastFolder
}

// SetLastFolder 记录文件夹并落盘。
//
// 同一路径重复调用（例如"开启共享"会再扫一次）直接返回，不产生写盘——这些
// 调用发生在用户点按钮的路径上，没必要每次都 fsync 一个没有变化的文件。
func (s *Store) SetLastFolder(folder string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.data.LastFolder == folder {
		return nil
	}
	prev := s.data.LastFolder
	// 先改内存再写盘：写失败要回滚，否则下次同路径调用会因"看起来没变"
	// 而直接返回，这个错误就再也没有重试机会了。
	s.data.LastFolder = folder

	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err == nil {
		err = writeFileAtomic(s.path, raw)
	}
	if err != nil {
		s.data.LastFolder = prev
		return err
	}
	return nil
}

// writeFileAtomic 先写临时文件并落盘，再 rename 原子替换目标。
//
// 与 internal/password 里的同名函数是同一个模式，刻意保留两份：它的全部内容
// 就是"tmp + Sync + rename"这十行，为一个工具函数新建一个包（或让 settings
// 依赖 password）都比重复更贵。崩溃时留下的必须是**旧文件**而不是截断的半个
// JSON——这里虽然只是"上次的目录"，但读回半个文件同样会静默丢掉记忆。
func writeFileAtomic(path string, data []byte) error {
	// 0600：文件里是用户本机的目录路径，属于不该被其他账户读到的东西
	f, err := os.OpenFile(path+".tmp", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(path + ".tmp")
		return err
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		os.Remove(path + ".tmp")
		return err
	}
	return nil
}
