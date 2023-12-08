package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/klog/v2"
)

const Async = "async"
const Sync = "sync"
const ImagePullTimeKey = "pull_duration_seconds"
const ImageMountTimeKey = "mount_duration_seconds"
const OperationErrorsCountKey = "operation_errors_total"

var ImagePullTime = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Subsystem: "warm_metal",
		Name:      ImagePullTimeKey,
		Help:      "The time it took to pull an image",
		Buckets:   []float64{0, 1, 5, 10, 15, 30, 60, 120, 180},
	},
	[]string{"operation_type"},
)

var ImageMountTime = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Subsystem: "warm_metal",
		Name:      ImageMountTimeKey,
		Help:      "The time it took to mount an image",
		Buckets:   []float64{0, 1, 5, 10, 15, 30, 60, 120, 180},
	},
	[]string{"operation_type"},
)

var OperationErrorsCount = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Subsystem: "warm_metal",
		Name:      OperationErrorsCountKey,
		Help:      "Cumulative number of operation (pull,mount,unmount) errors in the driver",
	},
	[]string{"operation_type"},
)

func RegisterMetrics() *prometheus.Registry {
	reg := prometheus.NewRegistry()
	reg.MustRegister(ImagePullTime)
	reg.MustRegister(ImageMountTime)
	reg.MustRegister(OperationErrorsCount)

	return reg
}

func StartMetricsServer(reg *prometheus.Registry) {
	go func() {
		http.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg}))
		klog.Info("serving internal metrics at port 8080")
		klog.Fatal(http.ListenAndServe(":8080", nil))
	}()
}
