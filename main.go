package main

import (
	"embed"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/jeessy2/ddns-go/v6/config"
	"github.com/jeessy2/ddns-go/v6/dns"
	"github.com/jeessy2/ddns-go/v6/util"
	"github.com/jeessy2/ddns-go/v6/web"
)

// ddns-go 版本
// ddns-go version
var versionFlag = flag.Bool("v", false, "ddns-go version")

// 监听地址
var listen = flag.String("l", ":9876", "Listen address")

// 更新频率(秒)
var every = flag.Int("f", 300, "Update frequency(seconds)")

// 缓存次数
var ipCacheTimes = flag.Int("cacheTimes", 5, "Cache times")

// 配置文件路径
var configFilePath = flag.String("c", util.GetConfigFilePathDefault(), "Custom configuration file path")

// Web 服务
var noWebService = flag.Bool("noweb", false, "No web service")

// 跳过验证证书
var skipVerify = flag.Bool("skipVerify", false, "Skip certificate verification")

// 自定义 DNS 服务器
var customDNS = flag.String("dns", "", "Custom DNS server address, example: 8.8.8.8")

// 重置密码
var newPassword = flag.String("resetPassword", "", "Reset password to the one entered")

//go:embed static
var staticEmbeddedFiles embed.FS

//go:embed favicon.ico
var faviconEmbeddedFile embed.FS

// version
var version = "DEV"

func main() {
	flag.Parse()
	if *versionFlag {
		fmt.Println(version)
		return
	}
	// 安卓 go/src/time/zoneinfo_android.go 固定localLoc 为 UTC
	if runtime.GOOS == "android" {
		util.FixTimezone()
	}
	// 检查监听地址
	if _, err := net.ResolveTCPAddr("tcp", *listen); err != nil {
		log.Fatalf("Parse listen address failed! Exception: %s", err)
	}
	// 设置版本号
	os.Setenv(web.VersionEnv, version)
	// 设置配置文件路径
	if *configFilePath != "" {
		absPath, _ := filepath.Abs(*configFilePath)
		os.Setenv(util.ConfigFilePathENV, absPath)
	}
	// 重置密码
	if *newPassword != "" {
		conf, err := config.GetConfigCached()
		if err == nil {
			conf.ResetPassword(*newPassword)
		} else {
			util.Log("配置文件 %s 不存在, 可通过-c指定配置文件", *configFilePath)
		}
		return
	}
	// 设置跳过证书验证
	if *skipVerify {
		util.SetInsecureSkipVerify()
	}
	// 设置自定义DNS
	if *customDNS != "" {
		util.SetDNS(*customDNS)
	}
	os.Setenv(util.IPCacheTimesENV, strconv.Itoa(*ipCacheTimes))
	// 兼容之前的配置文件
	conf, _ := config.GetConfigCached()
	conf.CompatibleConfig()
	// 初始化语言
	util.InitLogLang(conf.Lang)

	if !*noWebService {
		go func() {
			// 启动web服务
			err := runWebServer()
			if err != nil {
				log.Println(err)
				time.Sleep(time.Minute)
				os.Exit(1)
			}
		}()
	}

	// 初始化备用DNS
	util.InitBackupDNS(*customDNS, conf.Lang)

	// 等待网络连接
	util.WaitInternet(dns.Addresses)

	// 定时运行
	dns.RunTimer(time.Duration(*every) * time.Second)
}

func staticFsFunc(writer http.ResponseWriter, request *http.Request) {
	http.FileServer(http.FS(staticEmbeddedFiles)).ServeHTTP(writer, request)
}

func faviconFsFunc(writer http.ResponseWriter, request *http.Request) {
	http.FileServer(http.FS(faviconEmbeddedFile)).ServeHTTP(writer, request)
}

func runWebServer() error {
	// 启动静态文件服务
	http.HandleFunc("/static/", web.AuthAssert(staticFsFunc))
	http.HandleFunc("/favicon.ico", web.AuthAssert(faviconFsFunc))
	http.HandleFunc("/login", web.AuthAssert(web.Login))
	http.HandleFunc("/loginFunc", web.AuthAssert(web.LoginFunc))

	http.HandleFunc("/", web.Auth(web.Writing))
	http.HandleFunc("/save", web.Auth(web.Save))
	http.HandleFunc("/logs", web.Auth(web.Logs))
	http.HandleFunc("/clearLog", web.Auth(web.ClearLog))
	http.HandleFunc("/webhookTest", web.Auth(web.WebhookTest))
	http.HandleFunc("/logout", web.Auth(web.Logout))

	util.Log("监听 %s", *listen)

	l, err := net.Listen("tcp", *listen)
	if err != nil {
		return errors.New(util.LogStr("监听端口发生异常, 请检查端口是否被占用! %s", err))
	}

	// 没有配置, 自动打开浏览器
	autoOpenExplorer()

	return http.Serve(l, nil)
}

// 打开浏览器
func autoOpenExplorer() {
	_, err := config.GetConfigCached()
	// 未找到配置文件
	if err != nil {
		if util.IsRunInDocker() {
			// docker中运行, 提示
			util.Log("Docker中运行, 请在浏览器中打开 http://docker主机IP:9876 进行配置")
		} else {
			// 主机运行, 打开浏览器
			addr, err := net.ResolveTCPAddr("tcp", *listen)
			if err != nil {
				return
			}
			url := fmt.Sprintf("http://127.0.0.1:%d", addr.Port)
			if addr.IP.IsGlobalUnicast() {
				url = fmt.Sprintf("http://%s", addr.String())
			}
			go util.OpenExplorer(url)
		}
	}
}
