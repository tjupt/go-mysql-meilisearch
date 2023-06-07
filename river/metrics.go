package river

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	meiliInsertNum = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "mysql2meili_inserted_num",
			Help: "The number of docs inserted to meilisearch",
		}, []string{"index"},
	)
	meiliUpdateNum = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "mysql2meili_updated_num",
			Help: "The number of docs updated to meilisearch",
		}, []string{"index"},
	)
	meiliDeleteNum = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "mysql2meili_deleted_num",
			Help: "The number of docs deleted from meilisearch",
		}, []string{"index"},
	)
	canalSyncState = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "mysql2meili_canal_state",
			Help: "The canal slave running state: 0=stopped, 1=ok",
		},
	)
	canalDelay = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "mysql2meili_canal_delay",
			Help: "The canal slave lag",
		},
	)
)

func (r *River) collectMetrics() {
	for range time.Tick(10 * time.Second) {
		canalDelay.Set(float64(r.canal.GetDelay()))
	}
}

func InitStatus(addr string, path string) {
	http.Handle(path, promhttp.Handler())
	http.ListenAndServe(addr, nil)
}
