package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

const Async = "async"
const Sync = "sync"

var ImagePullTime = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Subsystem: "warm_metal",
		Name:      "pull_duration_seconds",
		Help:      "The time it took to pull an image",
		Buckets:   []float64{0, 1, 5, 10, 15, 30, 60, 120, 180},
	},
	[]string{"operation_type"},
)

var ImageMountTime = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Subsystem: "warm_metal",
		Name:      "mount_duration_seconds",
		Help:      "The time it took to mount an image",
		Buckets:   []float64{0, 1, 5, 10, 15, 30, 60, 120, 180},
	},
	[]string{"operation_type"},
)

var OperationErrorsCount = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Subsystem: "warm_metal",
		Name:      "operation_errors_total",
		Help:      "Cumulative number of operation (pull,mount,unmount) errors in the driver",
	},
	[]string{"operation_type"},
)
