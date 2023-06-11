package main

import (
	"flag"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/juju/errors"
	"github.com/siddontang/go-log/log"
	"github.com/tjupt/go-mysql-meilisearch/river"
)

var configFile = flag.String("config", "./etc/river.toml", "go-mysql-meilisearch config file")
var myAddr = flag.String("my_addr", "", "MySQL addr")
var myUser = flag.String("my_user", "", "MySQL user")
var myPass = flag.String("my_pass", "", "MySQL password")
var meiliAddr = flag.String("meili_addr", "", "meilisearch addr")
var meiliAPIKey = flag.String("meili_api_key", "", "meilisearch api key")
var dataDir = flag.String("data_dir", "", "path for go-mysql-meilisearch to save data")
var serverId = flag.Int("server_id", 0, "MySQL server id, as a pseudo slave")
var flavor = flag.String("flavor", "", "flavor: mysql or mariadb")
var execution = flag.String("exec", "", "mysqldump execution path")
var logLevel = flag.String("log_level", "info", "log level")

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())
	flag.Parse()

	log.SetLevelByName(*logLevel)

	sc := make(chan os.Signal, 1)
	signal.Notify(sc,
		os.Kill,
		os.Interrupt,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT)

	cfg, err := river.NewConfigWithFile(*configFile)
	if err != nil {
		println(errors.ErrorStack(err))
		return
	}

	if len(*myAddr) > 0 {
		cfg.MyAddr = *myAddr
	}

	if len(*myUser) > 0 {
		cfg.MyUser = *myUser
	}

	if len(*myPass) > 0 {
		cfg.MyPassword = *myPass
	}

	if *serverId > 0 {
		cfg.ServerID = uint32(*serverId)
	}

	if len(*meiliAddr) > 0 {
		cfg.MeiliAddr = *meiliAddr
	}

	if len(*meiliAPIKey) > 0 {
		cfg.MeiliAPIKey = *meiliAPIKey
	}

	if len(*dataDir) > 0 {
		cfg.DataDir = *dataDir
	}

	if len(*flavor) > 0 {
		cfg.Flavor = *flavor
	}

	if len(*execution) > 0 {
		cfg.DumpExec = *execution
	}

	r, err := river.NewRiver(cfg)
	if err != nil {
		println(errors.ErrorStack(err))
		return
	}

	done := make(chan struct{}, 1)
	go func() {
		r.Run()
		done <- struct{}{}
	}()

	select {
	case n := <-sc:
		log.Infof("receive signal %v, closing", n)
	case <-r.Ctx().Done():
		log.Infof("context is done with %v, closing", r.Ctx().Err())
	}

	r.Close()
	<-done
}