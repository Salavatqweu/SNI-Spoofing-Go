package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"sni-spoofing-go/config"
	"sni-spoofing-go/guiapi"
	"sni-spoofing-go/helper"
	"sni-spoofing-go/helper/spawn"
	"sni-spoofing-go/proxy"
)

type ProxyConfig = guiapi.ProxyConfig
type ProxyStatus = guiapi.ProxyStatus
type LogEvent = guiapi.LogEvent
type TestResult = guiapi.TestResult
type TestSummary = guiapi.TestSummary
type TestPreflight = guiapi.TestPreflight

type App struct { ctx context.Context; mu sync.Mutex; manager *helper.Manager; status ProxyStatus }
func NewApp() *App { return &App{} }
func (a *App) startup(ctx context.Context) { a.ctx=ctx; exe,err:=spawn.SelfExe(); if err!=nil {a.emitLog("error",fmt.Sprintf("locate executable: %v",err));return}; a.manager=helper.NewManager(exe,helper.EventHandler{OnLog:a.onHelperLog,OnStatus:a.onHelperStatus,OnTestResult:a.onHelperTestResult,OnDisconnect:a.onHelperDisconnect}) }
func (a *App) shutdown(ctx context.Context) { if a.manager!=nil {_=a.manager.Close()} }
func (a *App) onHelperLog(ev LogEvent){a.emitLog(ev.Level,ev.Message)}
func (a *App) onHelperStatus(st ProxyStatus){a.mu.Lock();a.status=st;a.mu.Unlock();a.emitStatus(st)}
func (a *App) onHelperTestResult(row TestResult){a.emitTestResult(row)}
func (a *App) onHelperDisconnect(err error){a.mu.Lock();active:=a.status.Running||a.status.Testing;a.mu.Unlock();if active&&err!=nil&&!errors.Is(err,io.EOF){a.emitLog("warn",fmt.Sprintf("helper disconnected: %v",err))};if active{a.clearStatus()}}
func (a *App) clearStatus(){a.mu.Lock();a.status=ProxyStatus{};a.mu.Unlock();a.emitStatus(a.status)}

func (a *App) configPath() (string,error){exe,err:=spawn.SelfExe();if err!=nil{return "",err};return filepath.Join(filepath.Dir(exe),"config.ini"),nil}
func userConfigPath() string { if v:=os.Getenv("APPDATA");v!="" {return filepath.Join(v,"SNI-Spoofing-Go","config.ini")}; if v:=os.Getenv("XDG_CONFIG_HOME");v!="" {return filepath.Join(v,"sni-spoofing-go","config.ini")}; if h,err:=os.UserHomeDir();err==nil{return filepath.Join(h,".config","sni-spoofing-go","config.ini")}; return "" }
func configCandidates(primary string) []string { out:=[]string{primary}; if p:=userConfigPath();p!=""&&p!=primary{out=append(out,p)};return out }

func applySavedOption(dst *ProxyConfig,opts config.FileOptions){if opts.Has("listen"){dst.Listen=opts.Listen};if opts.Has("connect"){dst.Connect=opts.Connect};if opts.Has("fake-sni"){dst.FakeSNI=opts.FakeSNI};if opts.Has("utls"){dst.UTLS=opts.UTLS};if opts.Has("injector"){dst.Injector=opts.Injector};if opts.Has("fake-repeat"){dst.FakeRepeat=opts.FakeRepeat};if opts.Has("fake-delay"){dst.FakeDelayMs=int(opts.FakeDelay/time.Millisecond)};if opts.Has("ack-timeout"){dst.AckTimeoutMs=int(opts.AckTimeout/time.Millisecond)};if opts.Has("enable-fragment"){dst.EnableFragment=opts.EnableFragment};if opts.Has("fragment-delay"){dst.FragmentDelayMs=int(opts.FragmentDelay/time.Millisecond)};if opts.Has("sni-chunk"){dst.SNIChunk=opts.SNIChunk}}
func (a *App) GetDefaultConfig() ProxyConfig {cfg:=guiapi.DefaultConfig(string(proxy.DefaultInjectorMode()));path,err:=a.configPath();if err!=nil{return cfg};for _,p:=range configCandidates(path){opts,err:=config.LoadFileOptions(p);if err==nil{applySavedOption(&cfg,opts);break}};return cfg}

func writeConfigFile(path,content string) error { if err:=os.MkdirAll(filepath.Dir(path),0755);err!=nil{return err}; if err:=os.WriteFile(path,[]byte(content),0644);err==nil{return nil}; tmp:=path+".tmp";if err:=os.WriteFile(tmp,[]byte(content),0644);err!=nil{return err};if err:=os.Rename(tmp,path);err!=nil{if removeErr:=os.Remove(path);removeErr!=nil{_ = os.Remove(tmp);return err};if retryErr:=os.Rename(tmp,path);retryErr!=nil{_ = os.Remove(tmp);return retryErr}};return nil }
func (a *App) SaveConfig(cfg ProxyConfig) error {if err:=guiapi.ValidateConfig(cfg);err!=nil{return err};primary,err:=a.configPath();if err!=nil{return err};content:=fmt.Sprintf("# SNI Spoofing GUI configuration\nlisten=%s\nconnect=%s\nfake-sni=%s\nutls=%s\ninjector=%s\nfake-repeat=%d\nfake-delay=%dms\nack-timeout=%dms\nenable-fragment=%t\nfragment-delay=%dms\nsni-chunk=%d\n",cfg.Listen,cfg.Connect,cfg.FakeSNI,cfg.UTLS,cfg.Injector,cfg.FakeRepeat,cfg.FakeDelayMs,cfg.AckTimeoutMs,cfg.EnableFragment,cfg.FragmentDelayMs,cfg.SNIChunk);var lastErr error;for _,p:=range configCandidates(primary){if err:=writeConfigFile(p,content);err==nil{a.emitLog("info",fmt.Sprintf("Configuration saved to %s",p));return nil}else{lastErr=err}};return fmt.Errorf("save config.ini: %w",lastErr)}

func (a *App) UTLSPresets() []string{return []string{"none","firefox","chrome","edge","safari","ios","qq","360browser"}}
func (a *App) InjectorModes() []string{return []string{"active","passive"}}
func (a *App) Status() ProxyStatus{a.mu.Lock();defer a.mu.Unlock();return a.status}
func (a *App) helperProgress() helper.ProgressFunc{return func(message string){a.emitLog("info",message)}}
func (a *App) helperClient(ctx context.Context)(*helper.Client,error){if a.manager==nil{return nil,errors.New("helper manager not initialized")};return a.manager.Ensure(ctx,a.helperProgress())}
func (a *App) Start(cfg ProxyConfig) error{if err:=guiapi.ValidateConfig(cfg);err!=nil{return err};a.emitLog("info","Starting proxy…");client,err:=a.helperClient(a.ctx);if err!=nil{return err};return client.Start(a.ctx,cfg)}
func (a *App) Stop() error{a.mu.Lock();active:=a.status.Running||a.status.Testing;mgr:=a.manager;a.mu.Unlock();if !active{return nil};if mgr==nil{a.clearStatus();return nil};if err:=mgr.Stop(a.ctx);err!=nil{if errors.Is(err,helper.ErrNotConnected){a.clearStatus();return nil};a.emitLog("warn",fmt.Sprintf("stop: %v",err))};return nil}
func (a *App) RunTest(cfg ProxyConfig)(TestSummary,error){if err:=guiapi.ValidateConfig(cfg);err!=nil{return TestSummary{},err};a.emitLog("info","Running test matrix…");client,err:=a.helperClient(a.ctx);if err!=nil{return TestSummary{},err};return client.RunTest(a.ctx,cfg)}
func (a *App) emitLog(level,message string){if a.ctx==nil{return};runtime.EventsEmit(a.ctx,"log",LogEvent{Level:level,Message:message})}
func (a *App) emitStatus(s ProxyStatus){if a.ctx==nil{return};runtime.EventsEmit(a.ctx,"status",s)}
func (a *App) emitTestResult(r TestResult){if a.ctx==nil{return};runtime.EventsEmit(a.ctx,"test_result",r)}
