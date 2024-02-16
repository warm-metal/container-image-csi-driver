package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/klog/v2"
)

const ImagePullTimeKey = "pull_duration_seconds"
const ImagePullTimeHistKey = "pull_duration_seconds_hist"
const ImagePullSizeKey = "pull_size_bytes"
const OperationErrorsCountKey = "operation_errors_total"

var ImagePullTimeHist = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Subsystem: "warm_metal",
		Name:      ImagePullTimeHistKey,
		Help:      "The time it took to pull an image",
		Buckets:   []float64{1, 5, 10, 15, 30, 60, 120, 300, 600, 900},
	},
	[]string{"error"},
)
var ImagePullTime = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Subsystem: "warm_metal",
		Name:      ImagePullTimeKey,
		Help:      "The time it took to mount an image",
	},
	[]string{"image", "error"},
)

var ImagePullSizeBytes = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Subsystem: "warm_metal",
		Name:      ImagePullSizeKey,
		Help:      "Size (in bytes) of pulled image",
	},
	[]string{"image"},
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
	reg.MustRegister(ImagePullTimeHist)
	reg.MustRegister(ImagePullSizeBytes)
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

func BoolToString(t bool) string {
	if t {
		return "true"
	} else {
		return "false"
	}
}
