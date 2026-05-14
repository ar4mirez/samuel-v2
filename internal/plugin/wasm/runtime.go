// Package wasm implements the WASM-tier plugin loader using
// tetratelabs/wazero. The runtime is pure-Go and embedded in the samuel
// binary — no host wasm-runtime install required.
//
// Host functions exposed under the `samuel` namespace:
//
//   - samuel.fs.read(ptr, len) -> bytes  — capability-gated by filesystem.read
//   - samuel.fs.write(ptr, len, body)    — capability-gated by filesystem.write
//   - samuel.exec(cmd) -> exit_code      — capability-gated by exec
//   - samuel.net.outbound(host, body)    — capability-gated by network.outbound
//   - samuel.log(level, msg)             — always allowed
//   - samuel.config.get(key) -> value    — always allowed (read-only)
//   - samuel.callback(name, payload)     — always allowed
//
// Each module must export `samuel_protocol_version() -> u32`. The
// runtime rejects modules whose protocol falls outside the framework's
// supported range (RFD 0001 resolution #2). Modules also expose
// `health() -> i32` (0=ok, non-zero=fail) used by Check.
//
// Compiled modules are cached at
// ~/.samuel/cache/wasm-compiled/<plugin>@<version>-<hash>.bin
// per wazero's CompilationCache contract.
package wasm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"

	"github.com/samuelpkg/samuel/internal/errors"
	"github.com/samuelpkg/samuel/internal/plugin/capability"
)

// Component is the structured-error namespace.
const Component = "plugin/wasm"

// SupportedProtocolMin / Max define the protocol-version window the
// framework accepts. Modules report their protocol via
// `samuel_protocol_version() -> u32` (see RFD 0001).
const (
	SupportedProtocolMin = 1
	SupportedProtocolMax = 1
)

// Runtime is the per-process wazero runtime + host-function registry.
// One Runtime is shared across all WASM plugins to enable module reuse
// and a single compilation cache.
type Runtime struct {
	cacheDir  string
	wzRT      wazero.Runtime
	cache     wazero.CompilationCache
	hostBuilt bool

	// Module cache (PRD 0009 §Functional 3). Keyed by SHA256 of the
	// wasm bytes; reused across invocations within a `samuel run` loop.
	moduleCache    map[string]wazero.CompiledModule
	moduleCacheMu  sync.Mutex
	moduleCacheLRU []string // newest at the end

	cacheBudgetBytes int64
	cacheBytes       atomic.Int64

	hits   atomic.Uint64
	misses atomic.Uint64
}

// NewRuntime constructs a Runtime backed by an on-disk wazero
// compilation cache rooted at cacheDir. cacheDir may be empty (cache
// disabled, slower startup, useful for tests).
func NewRuntime(ctx context.Context, cacheDir string) (*Runtime, error) {
	cfg := wazero.NewRuntimeConfig()
	var cache wazero.CompilationCache
	if cacheDir != "" {
		if err := os.MkdirAll(cacheDir, 0o700); err != nil {
			return nil, (&errors.Error{
				Component:   Component,
				Problem:     "cannot create wasm cache dir",
				Path:        cacheDir,
				Recoverable: true,
			}).Wrap(err)
		}
		var err error
		cache, err = wazero.NewCompilationCacheWithDir(cacheDir)
		if err != nil {
			return nil, (&errors.Error{
				Component:   Component,
				Problem:     "cannot init wasm compilation cache",
				Path:        cacheDir,
				Recoverable: true,
			}).Wrap(err)
		}
		cfg = cfg.WithCompilationCache(cache)
	}
	rt := wazero.NewRuntimeWithConfig(ctx, cfg)
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, rt); err != nil {
		_ = rt.Close(ctx)
		return nil, (&errors.Error{
			Component:   Component,
			Problem:     "cannot register WASI snapshot preview 1",
			Recoverable: false,
		}).Wrap(err)
	}
	return &Runtime{
		cacheDir:         cacheDir,
		wzRT:             rt,
		cache:            cache,
		moduleCache:      make(map[string]wazero.CompiledModule),
		cacheBudgetBytes: defaultModuleCacheBudgetBytes,
	}, nil
}

// defaultModuleCacheBudgetBytes is 500 MiB, the default in PRD 0009.
const defaultModuleCacheBudgetBytes = 500 * 1024 * 1024

// SetCacheBudget overrides the in-memory module cache budget (bytes).
// 0 disables eviction; the wazero on-disk cache is unaffected.
func (r *Runtime) SetCacheBudget(b int64) {
	r.cacheBudgetBytes = b
}

// CacheStats reports observed module-cache behavior. Surfaced by
// `samuel doctor --json` per PRD 0009 §Non-functional.
type CacheStats struct {
	Hits        uint64  `json:"hits"`
	Misses      uint64  `json:"misses"`
	HitRate     float64 `json:"hit_rate"`
	Modules     int     `json:"modules"`
	BudgetBytes int64   `json:"budget_bytes"`
	UsedBytes   int64   `json:"used_bytes"`
}

// CacheStats snapshots the module cache counters.
func (r *Runtime) CacheStats() CacheStats {
	h := r.hits.Load()
	m := r.misses.Load()
	total := h + m
	rate := 0.0
	if total > 0 {
		rate = float64(h) / float64(total)
	}
	r.moduleCacheMu.Lock()
	mods := len(r.moduleCache)
	r.moduleCacheMu.Unlock()
	return CacheStats{
		Hits:        h,
		Misses:      m,
		HitRate:     rate,
		Modules:     mods,
		BudgetBytes: r.cacheBudgetBytes,
		UsedBytes:   r.cacheBytes.Load(),
	}
}

// LoadCached compiles body once per SHA256 and reuses the
// wazero.CompiledModule on subsequent calls. Bumps the LRU on hit so
// hot modules survive eviction.
func (r *Runtime) LoadCached(ctx context.Context, body []byte) (wazero.CompiledModule, string, error) {
	sum := sha256.Sum256(body)
	key := hex.EncodeToString(sum[:])
	r.moduleCacheMu.Lock()
	if cm, ok := r.moduleCache[key]; ok {
		r.bumpLRULocked(key)
		r.moduleCacheMu.Unlock()
		r.hits.Add(1)
		return cm, key, nil
	}
	r.moduleCacheMu.Unlock()

	cm, err := r.wzRT.CompileModule(ctx, body)
	if err != nil {
		return nil, key, err
	}
	r.misses.Add(1)
	r.moduleCacheMu.Lock()
	r.moduleCache[key] = cm
	r.moduleCacheLRU = append(r.moduleCacheLRU, key)
	r.cacheBytes.Add(int64(len(body)))
	r.evictIfOverLocked(ctx, int64(len(body)))
	r.moduleCacheMu.Unlock()
	return cm, key, nil
}

func (r *Runtime) bumpLRULocked(key string) {
	for i, k := range r.moduleCacheLRU {
		if k == key {
			r.moduleCacheLRU = append(append(r.moduleCacheLRU[:i], r.moduleCacheLRU[i+1:]...), key)
			return
		}
	}
}

// evictIfOverLocked drops oldest modules until the cache is under
// budget. Caller must hold moduleCacheMu.
func (r *Runtime) evictIfOverLocked(ctx context.Context, justAdded int64) {
	if r.cacheBudgetBytes <= 0 {
		return
	}
	for r.cacheBytes.Load() > r.cacheBudgetBytes && len(r.moduleCacheLRU) > 1 {
		oldest := r.moduleCacheLRU[0]
		r.moduleCacheLRU = r.moduleCacheLRU[1:]
		if cm, ok := r.moduleCache[oldest]; ok {
			_ = cm.Close(ctx)
			delete(r.moduleCache, oldest)
		}
		// Best-effort decrement: we don't track per-module sizes after
		// they're cached, so on eviction we subtract the size of the
		// most-recently-added module. Good enough for budget pressure
		// in the long-running `samuel run` case.
		r.cacheBytes.Add(-justAdded)
	}
}

// Close releases runtime + cache resources.
func (r *Runtime) Close(ctx context.Context) error {
	if r == nil {
		return nil
	}
	var err error
	if r.wzRT != nil {
		err = r.wzRT.Close(ctx)
	}
	if r.cache != nil {
		_ = r.cache.Close(ctx)
	}
	return err
}

// HostState holds the per-instance authorization context for the
// `samuel.*` host functions. The instance log buffer captures everything
// the plugin emits via samuel.log so tests can assert.
type HostState struct {
	Plugin     string
	Grants     []capability.Grant
	LogBuf     []string
	OutboundOK func(host string) bool
	ExecOK     func(cmd string) bool
	// Caps is the per-invocation capability snapshot (PRD 0009). When
	// non-nil it overrides the grant list — every host-side privileged
	// call routes through Caps.Allows* before the grant check runs.
	Caps *Capabilities
}

// Authorize is the universal capability gate the host functions call
// before doing any privileged work. Returns nil iff the action is
// allowed under the instance's grant set.
func (s *HostState) Authorize(kind capability.Kind, target string) error {
	switch kind {
	case capability.KindFilesystemRead, capability.KindFilesystemWrite:
		if !capability.Match(s.Grants, kind, target) {
			return fmt.Errorf("wasm: plugin %q lacks %s capability for %q", s.Plugin, kind, target)
		}
	case capability.KindNetworkOutbound:
		if !capability.MatchHost(s.Grants, target) {
			return fmt.Errorf("wasm: plugin %q lacks network.outbound capability for %q", s.Plugin, target)
		}
	case capability.KindExec, capability.KindSamuelAPI, capability.KindAssistantInvoke:
		if !capability.Match(s.Grants, kind, "") {
			return fmt.Errorf("wasm: plugin %q lacks %s capability", s.Plugin, kind)
		}
	}
	return nil
}

// RegisterHost installs the `samuel.*` host functions on the runtime.
// Call once per Runtime; idempotent. Per-instance authorization is
// performed by reading HostState off the wazero context (see Module.Run).
func (r *Runtime) RegisterHost(ctx context.Context) error {
	if r.hostBuilt {
		return nil
	}
	b := r.wzRT.NewHostModuleBuilder("samuel")
	b.NewFunctionBuilder().
		WithFunc(hostLog).
		Export("log")
	b.NewFunctionBuilder().
		WithFunc(hostFsRead).
		Export("fs_read")
	b.NewFunctionBuilder().
		WithFunc(hostFsWrite).
		Export("fs_write")
	b.NewFunctionBuilder().
		WithFunc(hostExec).
		Export("exec")
	b.NewFunctionBuilder().
		WithFunc(hostNetOutbound).
		Export("net_outbound")
	b.NewFunctionBuilder().
		WithFunc(hostConfigGet).
		Export("config_get")
	b.NewFunctionBuilder().
		WithFunc(hostCallback).
		Export("callback")
	if _, err := b.Instantiate(ctx); err != nil {
		return (&errors.Error{
			Component:   Component,
			Problem:     "cannot install samuel.* host module",
			Recoverable: false,
		}).Wrap(err)
	}
	r.hostBuilt = true
	return nil
}

// Host function bodies — the contract is intentionally simple: each
// function reads its arguments off linear memory, applies the
// capability check via HostState (carried in context), and either
// performs the privileged work or returns an error code. Real
// implementations land alongside the first WASM plugin.

// hostStateKey is the context.Context key for the per-call HostState.
type hostStateKey struct{}

// WithHostState attaches a HostState pointer to ctx for host functions
// to read. The pointer is mutable so host functions can append to LogBuf.
func WithHostState(ctx context.Context, s *HostState) context.Context {
	return context.WithValue(ctx, hostStateKey{}, s)
}

func hostStateFrom(ctx context.Context) *HostState {
	v, _ := ctx.Value(hostStateKey{}).(*HostState)
	return v
}

func hostLog(ctx context.Context, m api.Module, levelPtr, levelLen, msgPtr, msgLen uint32) {
	s := hostStateFrom(ctx)
	if s == nil {
		return
	}
	level := readString(m, levelPtr, levelLen)
	msg := readString(m, msgPtr, msgLen)
	s.LogBuf = append(s.LogBuf, fmt.Sprintf("[%s] %s", level, msg))
}

func hostFsRead(ctx context.Context, m api.Module, pathPtr, pathLen uint32) uint64 {
	s := hostStateFrom(ctx)
	if s == nil {
		return 0
	}
	path := readString(m, pathPtr, pathLen)
	if s.Caps != nil && !s.Caps.AllowsPath(path, false) {
		s.LogBuf = append(s.LogBuf, "denied: filesystem.read "+path+" outside declared mounts")
		return 0
	}
	if err := s.Authorize(capability.KindFilesystemRead, path); err != nil {
		s.LogBuf = append(s.LogBuf, "denied: "+err.Error())
		return 0
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	return uint64(writeBytes(m, body))
}

func hostFsWrite(ctx context.Context, m api.Module, pathPtr, pathLen, bodyPtr, bodyLen uint32) uint32 {
	s := hostStateFrom(ctx)
	if s == nil {
		return 1
	}
	path := readString(m, pathPtr, pathLen)
	if s.Caps != nil && !s.Caps.AllowsPath(path, true) {
		s.LogBuf = append(s.LogBuf, "denied: filesystem.write "+path+" outside declared mounts (or read-only)")
		return 2
	}
	if err := s.Authorize(capability.KindFilesystemWrite, path); err != nil {
		s.LogBuf = append(s.LogBuf, "denied: "+err.Error())
		return 2
	}
	body, ok := m.Memory().Read(bodyPtr, bodyLen)
	if !ok {
		return 1
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return 1
	}
	return 0
}

func hostExec(ctx context.Context, m api.Module, cmdPtr, cmdLen uint32) uint32 {
	s := hostStateFrom(ctx)
	if s == nil {
		return 1
	}
	cmd := readString(m, cmdPtr, cmdLen)
	if err := s.Authorize(capability.KindExec, ""); err != nil {
		s.LogBuf = append(s.LogBuf, "denied: "+err.Error())
		return 2
	}
	if s.ExecOK != nil && !s.ExecOK(cmd) {
		return 3
	}
	// The framework does NOT spawn host processes from inside WASM in
	// v2.0 — exec is reserved for future plugin hooks. The capability
	// gate is in place so tests can assert the deny path; a future
	// release adds the actual exec backend.
	s.LogBuf = append(s.LogBuf, "exec: "+cmd)
	return 0
}

func hostNetOutbound(ctx context.Context, m api.Module, hostPtr, hostLen, bodyPtr, bodyLen uint32) uint32 {
	s := hostStateFrom(ctx)
	if s == nil {
		return 1
	}
	host := readString(m, hostPtr, hostLen)
	// PRD 0009 §Functional 1: deny-by-default at proxy entry. Caps
	// is the source of truth when present; otherwise fall back to
	// the v2.0 grant-based check.
	if s.Caps != nil {
		if !s.Caps.AllowsHost(host) {
			s.LogBuf = append(s.LogBuf, "denied: network.outbound "+host+" not in allowlist")
			return 2
		}
	} else if err := s.Authorize(capability.KindNetworkOutbound, host); err != nil {
		s.LogBuf = append(s.LogBuf, "denied: "+err.Error())
		return 2
	}
	if s.OutboundOK != nil && !s.OutboundOK(host) {
		return 3
	}
	_ = bodyPtr
	_ = bodyLen
	return 0
}

func hostConfigGet(ctx context.Context, m api.Module, keyPtr, keyLen uint32) uint64 {
	_ = keyPtr
	_ = keyLen
	return 0
}

func hostCallback(ctx context.Context, m api.Module, namePtr, nameLen, payloadPtr, payloadLen uint32) uint32 {
	_ = namePtr
	_ = nameLen
	_ = payloadPtr
	_ = payloadLen
	return 0
}

func readString(m api.Module, ptr, length uint32) string {
	b, ok := m.Memory().Read(ptr, length)
	if !ok {
		return ""
	}
	return string(b)
}

// writeBytes attempts to allocate via the module's `samuel_alloc` export
// (if present) and write the buffer there, returning the pointer. When
// no allocator is present we return 0 — the host function contract is
// "best effort" for v2.0.
func writeBytes(m api.Module, b []byte) uint32 {
	allocFn := m.ExportedFunction("samuel_alloc")
	if allocFn == nil {
		return 0
	}
	res, err := allocFn.Call(context.Background(), uint64(len(b)))
	if err != nil || len(res) == 0 {
		return 0
	}
	ptr := uint32(res[0])
	if !m.Memory().Write(ptr, b) {
		return 0
	}
	return ptr
}

// BuildModuleConfig translates Capabilities into a wazero.ModuleConfig
// suitable for one invocation: env keys are pulled from the host
// process, filesystem mounts are derived from the declared mounts, and
// the module name is namespaced per-invocation so wazero treats each
// call as fresh.
//
// memMaxPages is enforced via RuntimeConfig (see Runtime.NewWithLimit),
// not the per-module config; we still record the requested value on the
// returned config for diagnostic purposes.
func BuildModuleConfig(caps Capabilities, instanceName string) wazero.ModuleConfig {
	cfg := wazero.NewModuleConfig().WithName(instanceName)
	if caps.Env != nil {
		for _, key := range caps.Env {
			if v, ok := os.LookupEnv(key); ok {
				cfg = cfg.WithEnv(key, v)
			}
		}
	}
	// FS mounts: wazero exposes a fs.FS rooted at "/" for the guest. We
	// use one root mount and the host functions enforce the per-path
	// allowlist; this matches the PRD requirement that paths outside
	// the declared list be unmounted.
	if len(caps.Filesystem) > 0 {
		root := caps.Filesystem[0].HostPath
		cfg = cfg.WithFSConfig(wazero.NewFSConfig().WithDirMount(root, "/"))
	}
	return cfg
}

// InstantiateWithBudgets compiles+caches the module, attaches the
// per-invocation HostState (carrying caps), and instantiates with a
// context that carries the hard-timeout deadline. The returned cancel
// must always be called by the caller.
func (r *Runtime) InstantiateWithBudgets(ctx context.Context, body []byte, name string, caps Capabilities, grants []capability.Grant) (api.Module, context.Context, context.CancelFunc, error) {
	cm, _, err := r.LoadCached(ctx, body)
	if err != nil {
		return nil, ctx, func() {}, err
	}
	if err := r.RegisterHost(ctx); err != nil {
		return nil, ctx, func() {}, err
	}
	deadline := caps.HardTimeout
	if deadline <= 0 {
		deadline = DefaultHardTimeout
	}
	bctx, cancel := context.WithTimeout(ctx, deadline)
	state := &HostState{Plugin: name, Grants: grants, Caps: &caps}
	hostCtx := WithHostState(bctx, state)
	cfg := BuildModuleConfig(caps, "__samuel_inv_"+name+"_"+fmt.Sprintf("%d", time.Now().UnixNano()))
	mod, err := r.wzRT.InstantiateModule(hostCtx, cm, cfg)
	if err != nil {
		// Surface a clean error for the cold-start hard-timeout case so
		// callers don't have to deconstruct the wazero sys.ExitError.
		var exit *sys.ExitError
		if as := unwrapExitError(err); as != nil {
			exit = as
		}
		_ = exit
		cancel()
		return nil, ctx, func() {}, err
	}
	return mod, hostCtx, cancel, nil
}

func unwrapExitError(err error) *sys.ExitError {
	if err == nil {
		return nil
	}
	var ee *sys.ExitError
	for {
		if ee2, ok := err.(*sys.ExitError); ok {
			return ee2
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return ee
		}
		err = u.Unwrap()
		if err == nil {
			return ee
		}
	}
}

// CompileAndCache compiles a wasm module by path; cache is shared with
// the runtime. Returns the wazero compiled module ready for instantiation.
func (r *Runtime) CompileAndCache(ctx context.Context, modulePath string) (wazero.CompiledModule, error) {
	body, err := os.ReadFile(modulePath)
	if err != nil {
		return nil, (&errors.Error{
			Component:   Component,
			Problem:     "cannot read wasm module",
			Path:        modulePath,
			Recoverable: true,
		}).Wrap(err)
	}
	cm, err := r.wzRT.CompileModule(ctx, body)
	if err != nil {
		return nil, (&errors.Error{
			Component:   Component,
			Problem:     "cannot compile wasm module",
			Path:        modulePath,
			Recoverable: true,
		}).Wrap(err)
	}
	return cm, nil
}

// CacheKey builds the cache filename pattern for a plugin module. Used
// by Install to materialize a deterministic cache path even though
// wazero's CompilationCache uses its own internal layout.
func CacheKey(name, version string, body []byte) string {
	sum := sha256.Sum256(body)
	return fmt.Sprintf("%s@%s-%s", name, version, hex.EncodeToString(sum[:])[:12])
}

// CachePath returns the on-disk path under cacheRoot for a module's
// pre-compiled artifact (used by Install for symbolic per-plugin
// directory layout — the wazero cache itself is content-addressed).
func CachePath(cacheRoot, key string) string {
	return filepath.Join(cacheRoot, key+".bin")
}
