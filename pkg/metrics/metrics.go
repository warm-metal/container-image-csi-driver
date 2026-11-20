package metrics

import (
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/klog/v2"
)

const ImagePullTimeKey = "pull_duration_seconds"
const ImagePullTimeHistKey = "pull_duration_seconds_hist"
const ImagePullSizeKey = "pull_size_bytes"
const OperationErrorsCountKey = "operation_errors_total"
const ImagePullInFlightKey = "image_pull_inflight"
const ImagePullQueuedKey = "image_pull_queued"

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

// ImagePullInFlight reports the number of image pulls currently holding a
// concurrency slot (i.e. actively calling the container runtime's PullImage),
// when --max-concurrent-image-pulls is set to a value > 0. It stays at 0 when
// bounded parallelism is disabled (the default), since no slot bookkeeping
// occurs in that case.
var ImagePullInFlight = prometheus.NewGauge(
	prometheus.GaugeOpts{
		Subsystem: "warm_metal",
		Name:      ImagePullInFlightKey,
		Help:      "Number of image pulls currently holding a concurrency slot (actively pulling). Only meaningful when --max-concurrent-image-pulls > 0.",
	},
)

// ImagePullQueued reports the number of image pulls currently waiting for a
// free concurrency slot before they can start pulling, when
// --max-concurrent-image-pulls is set to a value > 0.
var ImagePullQueued = prometheus.NewGauge(
	prometheus.GaugeOpts{
		Subsystem: "warm_metal",
		Name:      ImagePullQueuedKey,
		Help:      "Number of image pulls currently queued, waiting for a free concurrency slot. Only meaningful when --max-concurrent-image-pulls > 0.",
	},
)

func RegisterMetrics() *prometheus.Registry {
	reg := prometheus.NewRegistry()
	reg.MustRegister(ImagePullTime)
	reg.MustRegister(ImagePullTimeHist)
	reg.MustRegister(ImagePullSizeBytes)
	reg.MustRegister(OperationErrorsCount)
	reg.MustRegister(ImagePullInFlight)
	reg.MustRegister(ImagePullQueued)

	return reg
}

func StartMetricsServer(reg *prometheus.Registry, port int) {
	go func() {
		http.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg}))
		klog.Infof("serving internal metrics at port %d", port)
		klog.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))
	}()
}

func BoolToString(t bool) string {
	if t {
		return "true"
	} else {
		return "false"
	}
}
