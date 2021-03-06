package discovery

import (
	"encoding/json"
	"fmt"
	"github.com/cxqlkk/library/discovery/consul/config"
	"github.com/cxqlkk/library/log"
	"github.com/hashicorp/consul/api"
	"go.uber.org/zap"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"sync/atomic"
	"time"
)

const (
	_serviceInitRetryTime     = 3
	_serviceCheckInterval     = time.Second * 10
	_serviceInitRetryInterval = 1 * time.Second
)

type Service struct {
	value        atomic.Value
	consulClient *api.Client
	//lastIndex    uint64

	//监测服务
	lastHealthIndex uint64
}

func NewService() *Service {
	var srv = &Service{
		value:        atomic.Value{},
		consulClient: config.GetConsulClient(),
	}
	if err := srv.initService(); err != nil {
		panic(fmt.Errorf("初始化service 错误:%v", err))
	}
	go srv.serviceCheck()
	return srv
}

func (srv *Service) Call(serviceName, path string, rw http.ResponseWriter, req *http.Request) error {
	services := srv.value.Load().(map[string][]string)
	if addrs, ok := services[serviceName]; ok {
		var addr = Random(addrs)
		if remote, err := url.Parse(addr); err != nil {
			log.Logger.Error("srv.Proxy() 服务地址解析失败:", zap.Error(fmt.Errorf("serverName:%s addr:%s %w", serviceName, addr, err)))
			return err
		} else {
			req.URL.Path = path
			proxy := NewSingleHostReverseProxy(remote)
			proxy.ErrorHandler = func(writer http.ResponseWriter, request *http.Request, e error) {
				log.Logger.Error("srv.Proxy()", zap.Error(err))
				writer.WriteHeader(200)
				json.NewEncoder(writer).Encode(map[string]interface{}{
					"error_code": 1005,
					"message":    fmt.Sprintf("服务访问错误:servername:%s,serveraddr:%s,err:%s", serviceName, addr, e.Error()),
				})
			}
			proxy.ServeHTTP(rw, req)
			return nil
		}
	}
	log.Logger.Error("srv.Proxy()", zap.Error(fmt.Errorf("%s", "服务不存在")))
	return fmt.Errorf("%s", "服务不存在")
}

func (srv *Service) initService() (err error) {
	var services = map[string][]string{}
	//var meta *api.QueryMeta
	for i := 0; i < _serviceInitRetryTime; i++ {
		if services, _, err = srv.consulClient.Catalog().Services(nil); err == nil {
			//srv.lastIndex = meta.LastIndex
			return srv.loadServices(services)
		}
		log.Logger.Error("srv.initService()", zap.Error(err))
	}
	return err
}
func (srv *Service) loadServices(serves map[string][]string) (err error) {
	var serviceMap = map[string][]string{}
	for k, _ := range serves {
		if entry, _, err := srv.consulClient.Health().Service(k, "", true, nil); err != nil {
			log.Logger.Error("srv.loadServices()", zap.Error(err))
			return err
		} else {
			//todo fixme
			for _, ele := range entry {
				serviceMap[k] = append(serviceMap[k], "http://"+ele.Service.Address+":"+strconv.Itoa(ele.Service.Port))
			}
		}
	}
	srv.value.Store(serviceMap)
	return nil
}

func (srv *Service) serviceCheck() {
	//configTimer := time.NewTicker(_serviceCheckInterval)
	//for {
	//	select {
	//	case <-configTimer.C:
	//		if _, healthMeta, err := srv.consulClient.Health().State(api.HealthPassing, &api.QueryOptions{
	//			WaitIndex: srv.lastHealthIndex,
	//			WaitTime:  100 * time.Second,
	//		}); err != nil {
	//			log.Logger.Error("srv.serviceCheck()", zap.Error(err))
	//		} else if healthMeta.LastIndex != srv.lastHealthIndex {
	//			if services, _, err := srv.consulClient.Catalog().Services(nil); err != nil {
	//				log.Logger.Error("srv.serviceCheck()", zap.Error(err))
	//			} else {
	//				srv.loadServices(services)
	//			}
	//			srv.lastHealthIndex = healthMeta.LastIndex
	//		}
	//
	//	}
	//}
	for {
		log.Logger.Info("开始检查服务更新", zap.String("now", time.Now().Format("2006-01-02 15:04:05")))
		if _, healthMeta, err := srv.consulClient.Health().State(api.HealthPassing, &api.QueryOptions{
			WaitIndex: srv.lastHealthIndex,
			WaitTime:  100 * time.Second,
		}); err != nil {
			log.Logger.Error("srv.serviceCheck()", zap.Error(err))
		} else if healthMeta.LastIndex != srv.lastHealthIndex {
			log.Logger.Info("服务检查存在更新", zap.String("now", time.Now().Format("2006-01-02 15:04:05")), zap.Int("srv.lastHealthIndex", int(srv.lastHealthIndex)), zap.Int("healthMeta.LastInde", int(healthMeta.LastIndex)))
			if services, _, err := srv.consulClient.Catalog().Services(nil); err != nil {
				log.Logger.Error("srv.serviceCheck()", zap.Error(err))
			} else {
				srv.loadServices(services)
			}
			srv.lastHealthIndex = healthMeta.LastIndex
		}
		log.Logger.Info("服务检查完毕", zap.String("now", time.Now().Format("2006-01-02 15:04:05")))
		time.Sleep(_serviceCheckInterval) //避免出现 意外情况，还是先睡一会
	}
}

//随机
func Random(addrs []string) string {
	if len(addrs) == 0 {
		return ""
	}
	return addrs[rand.Intn(len(addrs))]
}
