package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/version"
)

func init() {
	prometheus.MustRegister(version.NewCollector("sql_exporter"))
}

func main() {
	var (
		showVersion   = flag.Bool("version", false, "Print version information.")
		listenAddress = flag.String("web.listen-address", ":9237", "Address to listen on for web interface and telemetry.")
		metricsPath   = flag.String("web.telemetry-path", "/metrics", "Path under which to expose metrics.")
		configFile    = flag.String("config.file", os.Getenv("CONFIG"), "SQL Exporter configuration file name.")
		configCheck   = flag.Bool("config.check", false, "SQL Exporter check configuration file.")
		check         = flag.Bool("check", false, "SQL Exporter check exporter, jobs and queries.")
		historyLimit  = flag.Uint("history.limit", 100, "SQL Exporter check exporter, jobs and queries.")
	)

	flag.Parse()

	if *showVersion {
		fmt.Fprintln(os.Stdout, version.Print("sql_exporter"))
		os.Exit(0)
	}

	// init logger
	logger := log.NewJSONLogger(os.Stdout)
	logger = log.With(logger,
		"ts", log.DefaultTimestampUTC,
		"caller", log.DefaultCaller,
	)
	// set the allowed log level filter
	switch strings.ToLower(os.Getenv("LOGLEVEL")) {
	case "debug":
		logger = level.NewFilter(logger, level.AllowDebug())
	case "info":
		logger = level.NewFilter(logger, level.AllowInfo())
	case "warn":
		logger = level.NewFilter(logger, level.AllowWarn())
	case "error":
		logger = level.NewFilter(logger, level.AllowError())
	default:
		logger = level.NewFilter(logger, level.AllowAll())
	}

	logger.Log("msg", "Starting sql_exporter", "version_info", version.Info(), "build_context", version.BuildContext())

	expLogger := newRotationLogger(logger, *historyLimit)

	exporter, err := NewExporter(expLogger, *configFile)
	if err != nil {
		level.Error(logger).Log("msg", "Error starting exporter", "err", err)
		os.Exit(1)
	}
	prometheus.MustRegister(exporter)

	if *configCheck {
		logger.Log("msg", "Config file is ok exiting...")
		os.Exit(0)
	}

	if *check {
		expLogger.SetLogger(level.NewFilter(logger, level.AllowWarn()))
		exporter.RunOnce()
		if expLogger.GerErrorsCount() > 0 {
			os.Exit(1)
		}
		os.Exit(0)
	}
	exporter.Run()

	// setup and start webserver
	http.Handle(*metricsPath, promhttp.Handler())
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "OK", http.StatusOK) })

	http.HandleFunc("/query_logs", func(w http.ResponseWriter, r *http.Request) {
		job := r.URL.Query().Get("job")
		query := r.URL.Query().Get("query")
		w.Write([]byte(`<html>
		<head><title>SQL Exporter</title></head>
		<body>
		<h1>SQL Exporter</h1>`))
		found := false
		for _, j := range exporter.jobs {
			if job == j.Name {
				for _, q := range j.Queries {
					if q.Name == query {
						fmt.Fprintf(w, "<h2>Logs for fob='%s' query='%s'</h2>",
							j.Name, q.Name)
						for _, msg := range q.log.GetHistory() {
							found = true
							w.Write([]byte("<p>"))
							w.Write([]byte(msg))
							w.Write([]byte("</p>"))
						}
					}
				}
			}
		}
		if !found {
			fmt.Fprintf(w, "Logs not found. Please see <a href='job_logs?job=%s'>job logs</a>",
				job)
		}
		w.Write([]byte(`</body>
		</html>`))
	})

	http.HandleFunc("/job_logs", func(w http.ResponseWriter, r *http.Request) {
		job := r.URL.Query().Get("job")
		w.Write([]byte(`<html>
		<head><title>SQL Exporter</title></head>
		<body>
		<h1>SQL Exporter</h1>`))
		found := false
		for _, j := range exporter.jobs {
			if job == j.Name {
				found = true
				fmt.Fprintf(w, "<h2>Logs for job='%s'</h2>",
					j.Name)
				for _, msg := range j.log.GetHistory() {
					w.Write([]byte("<p>"))
					w.Write([]byte(msg))
					w.Write([]byte("</p>"))
				}
			}
		}
		if !found {
			w.Write([]byte("Logs not found. Please see logs on logs server"))
		}
		w.Write([]byte(`</body>
		</html>`))
	})
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
		<head><title>SQL Exporter</title></head>
		<body>
		<h1>SQL Exporter</h1>
		<p><a href="` + *metricsPath + `">Metrics</a></p>
		<h2>Logs</h2>
		<table border='1'><tr><th>Job</th><th>Query</th><th>Status</th><th>Logs</th></tr>`))
		for _, j := range exporter.jobs {
			for _, q := range j.Queries {

				success := "Success"
				if q.log.GetLastIsError() {
					success = "<strong>Failure</strong>"
				}
				fmt.Fprintf(w, "<tr><td>%s</td><td>%s</td><td>%s</td><td><a href='query_logs?job=%s&query=%s'>Logs</a></td></td>",
					j.Name, q.Name, success, j.Name, q.Name)
			}
		}

		w.Write([]byte(`</table>
		</body>
		</html>
		`))
	})

	level.Info(logger).Log("msg", "Listening", "listenAddress", *listenAddress)
	if err := http.ListenAndServe(*listenAddress, nil); err != nil {
		level.Error(logger).Log("msg", "Error starting HTTP server:", "err", err)
		os.Exit(1)
	}
}
