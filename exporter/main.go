package main

import (
	"bytes"
	"flag"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/version"
	"github.com/uber/jaeger-client-go"
	"github.com/uber/jaeger-client-go/config"
	ozonTracing "gitlab.ozon.ru/platform/tracer-go"
	"gopkg.in/yaml.v2"
)

const indexPage string = `<html>
<head><title>SQL Exporter</title></head>
<body>
<h1>SQL Exporter</h1>
<p><a href="{{.metricsPath}}">Metrics</a></p>
<p><a href="/job_test">Job test</a></p>
<h2>Logs</h2>
<table border='1'><tr><th>Job</th><th>Job Status</th><th>Job Logs</th><th>Query</th><th>Query Status</th><th>Query Logs</th></tr>
{{range .jobs}}
	{{$job := .}}
	{{range .Queries}}
		<tr>
			<td>
				{{$job.Name}}
			</td>
			<td>
				{{if $job.Logger.LastIsError}}
				<strong>Failure</strong>
				{{else}}
				Success
				{{end}}
			</td>
			<td>
				<a href='job_logs?job={{$job.Name}}'>Job Logs</a>
			</td>
			<td>
				{{.Name}}
			</td>
			<td>
				{{if .Logger.LastIsError}}
				<strong>Failure</strong>
				{{else}}
				Success
				{{end}}
			</td>
			<td>
				<a href='query_logs?job={{$job.Name}}&query={{.Name}}'>Query Logs</a>
			</td>
		</tr>
	{{end}}
{{end}}
</body>
</html>`

const jobLogsPage string = `<html>
<head><title>SQL Exporter</title></head>
<body>
<h1>SQL Exporter</h1>
<h2>Logs for job={{.jobName}}</h2>
{{if .found}}
	{{if .logs}}
		{{range .logs}}
		<p>{{.}}</p>
		{{end}}
	{{else}}
		<p>Job logs is empty. Please see logs on logs server</p>
	{{end}}
{{else}}
	<p>Job not found. Please see logs on logs server</p>
{{end}}
</body>
</html>`

const queryLogsPage string = `<html>
<head><title>SQL Exporter</title></head>
<body>
<h1>SQL Exporter</h1>
<h2>Logs for job={{.jobName}} query={{.queryName}}</h2>
{{if .found}}
	{{if .logs}}
		{{range .logs}}
		<p>{{.}}</p>
		{{end}}
	{{else}}
		<p>Query logs is empty. Please see <a href='job_logs?job={{.jobName}}'>Job logs</a></p>
	{{end}}
{{else}}
	<p>Job/query not found. Please see logs on logs server</p>
{{end}}
</body>
</html>`

const jobTestPageTemplate string = `<html>
<head>
	<title>SQL Exporter</title>
	<link rel="stylesheet" href="https://stackpath.bootstrapcdn.com/bootstrap/4.1.3/css/bootstrap.min.css" integrity="sha384-MCw98/SFnGE8fJT3GXwEOngsV7Zt27NXFoaoApmYm81iuXoPkFOJwJ8ERdknLPMO" crossorigin="anonymous">
</head>
<body>
<div class="container-fluid">
<h1>SQL Exporter</h1>
<h2>Check your job</h2>
{{if .logs}}
<h3>Config</h3>
<pre>{{ .config }}</pre>
<h3>Logs</h3>
{{range .logs}}
<p>{{.}}</p>
{{end}}
{{if .metrics}}
<h3>Metrics</h3>
<pre style="word-wrap: break-word; white-space: pre-wrap;">{{ .metrics }}</pre>
{{end}}
{{else}}
<form method="POST">

<div class="form-group">
	<label for="name">Job name</label>
	<input type="text" class="form-control" name="name" id="name" placeholder="job_name" required>
	<small id="nameHelp" class="form-text text-muted">Each job needs a unique name, it's used for logging and as an default label.</small>
</div>

<div class="form-group">
	<label for="interval">Job interval</label>
	<input type="text" class="form-control" name="interval" id="interval" placeholder="5m" required>
	<small id="intervalHelp" class="form-text text-muted">Interval defined the pause between the runs of this job.</small>
</div>

<div class="form-group">
	<label for="connections">Job connections</label>
	<input type="text" class="form-control" name="connections" id="connections" placeholder="sqlserver://usr:pswd@host1 sqlserver://usr:pswd@host2" required>
	<small id="connectionsHelp" class="form-text text-muted">Connections is an array of connection URLs. Each query will be executed on each connection. Use space separation. Example: 'sqlserver://usr:pswd@host1 sqlserver://usr:pswd@host2'.</small>
</div>

<div class="form-group">
	<label for="queryName">Query name</label>
	<input type="text" class="form-control" name="query.name" id="queryName" placeholder="query_name" required>
	<small id="queryNameHelp" class="form-text text-muted">Query name is prefied with 'sql_' and used as the metric name.</small>
</div>

<div class="form-group">
	<label for="queryHelp">Query help</label>
	<input type="text" class="form-control" name="query.help" id="queryHelp" placeholder="query help" required>
	<small id="queryNameHelp" class="form-text text-muted">Help is a requirement of the Prometheus default registry, currently not used by the Prometheus server. <red>Important: Must be the same for all metrics with the same name!</red></small>
</div>
<div class="form-group">
	<label for="labels">Query labels</label>
	<input type="text" class="form-control" name="query.labels" id="labels" placeholder="label1 label2 label3">
	<small id="labelsHelp" class="form-text text-muted">Labels is an array of columns which will be used as additional labels. <red>Must be the same for all metrics with the same name! All labels columns should be of type text, varchar or string</red>.</small>
</div>

<div class="form-group">
	<label for="values">Query values</label>
	<input type="text" class="form-control" name="query.values" id="values" placeholder="value1 value2 value3">
	<small id="valuesHelp" class="form-text text-muted">Values is an array of columns used as metric values. All values should be of type float.</small>
</div>

<div class="form-group">
	<label for="query">Query</label>
	<textarea rows=3 class="form-control" name="query.query" id="query" placeholder="sql statement"></textarea>
	<small id="queryHelp" class="form-text text-muted">Query is the SQL query that is run unalterted on the each of the connections for this job.</small>
</div>
<button type="submit" class="btn btn-primary">Submit</button>
</form>
{{end}}
</div>
</body>
</html>`

func jobTestPageHandler(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	t := template.New("job_test")
	t, _ = t.Parse(jobTestPageTemplate)
	data := make(map[string]interface{})
	defer t.Execute(w, data)
	if r.Form.Get("name") == "" {
		return
	}
	var labels []string
	if r.Form.Get("query.labels") != "" {
		labels = strings.Split(r.Form.Get("query.labels"), " ")
	}
	query := &Query{
		Name:   r.Form.Get("query.name"),
		Help:   r.Form.Get("query.help"),
		Labels: labels,
		Values: strings.Split(r.Form.Get("query.values"), " "),
		Query:  r.Form.Get("query.query"),
	}
	interval, _ := time.ParseDuration(r.Form.Get("interval"))
	job := &Job{
		Name:        r.Form.Get("name"),
		Interval:    interval,
		Connections: strings.Split(r.Form.Get("connections"), " "),
		Queries:     []*Query{query},
	}
	logger := log.NewJSONLogger(os.Stdout)
	logger = log.With(logger,
		"ts", log.DefaultTimestampUTC,
		"caller", log.DefaultCaller,
	)
	expLogger := newRotationLogger(logger, 100)
	tracer := opentracing.GlobalTracer()
	job.Init(tracer, expLogger, make(map[string]string))
	job.Prepare()
	checkExporter := &Exporter{
		jobs:   []*Job{job},
		logger: expLogger,
	}
	checkExporter.RunOnce()
	data["logs"] = job.Logger.GetHistory()
	checkPrometheusReg := prometheus.NewRegistry()
	checkPrometheusReg.MustRegister(checkExporter)
	var f File
	f.Jobs = []*Job{job}
	config, _ := yaml.Marshal(f)
	data["config"] = string(config)
	defer checkPrometheusReg.Unregister(checkExporter)
	gathering, err := checkPrometheusReg.Gather()
	if err != nil {
		level.Error(job.Logger).Log("msg", "Error get Prometheus Gathers", "err", err)
		return
	}
	out := &bytes.Buffer{}
	for _, mf := range gathering {
		if _, err := expfmt.MetricFamilyToText(out, mf); err != nil {
			level.Error(job.Logger).Log("msg", "Error read metrics from Prometheus Gathers", "err", err)
		}
	}
	data["metrics"] = out
}

func init() {
	prometheus.MustRegister(version.NewCollector("sql_exporter"))
}

func main() {
	var (
		showVersion   = flag.Bool("version", false, "Print version information.")
		listenAddress = flag.String("web.listen-address", ":9237", "Address to listen on for web interface and telemetry.")
		metricsPath   = flag.String("web.telemetry-path", "/metrics", "Path under which to expose metrics.")
		configFile    = flag.String("config.file", os.Getenv("CONFIG"), "SQL Exporter configuration file name.")
		configCheck   = flag.Bool("config.check", false, "Check configuration file structure.")
		check         = flag.Bool("check", false, "Check exporter, jobs and queries.")
		historyLimit  = flag.Uint("history.limit", 100, "History limit for jobs/query logs in web-UI.")
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

	if tracingServiceName := os.Getenv("JAEGER_SERVICE_NAME"); tracingServiceName == "" {
		jaegerServiceName := "sql_exporter"
		if hostname, err := os.Hostname(); err == nil {
			jaegerServiceName = fmt.Sprintf("%s_at_%s", jaegerServiceName, hostname)
		}

		os.Setenv("JAEGER_SERVICE_NAME", jaegerServiceName)
	}

	var tracer opentracing.Tracer

	// Setup optional tracing.
	{
		closer, err := ozonTracing.Init(config.Logger(jaeger.StdLogger))
		if err != nil {
			fmt.Fprintln(os.Stderr, errors.Wrap(err, "Error initializing tracer"))
			os.Exit(1)
		}
		defer closer.Close()

		tracer = opentracing.GlobalTracer()
	}

	exporter, err := NewExporter(tracer, expLogger, *configFile)
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
		if expLogger.errorCounter > 0 {
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
		t := template.New("query_logs")
		t, _ = t.Parse(queryLogsPage)
		data := make(map[string]interface{})
		data["jobName"] = job
		data["queryName"] = query
		data["found"] = false
		for _, j := range exporter.jobs {
			if job == j.Name {
				for _, q := range j.Queries {
					if q.Name == query {
						data["found"] = true
						data["logs"] = q.Logger.GetHistory()
					}
				}
			}
		}
		t.Execute(w, data)
	})

	http.HandleFunc("/job_logs", func(w http.ResponseWriter, r *http.Request) {
		job := r.URL.Query().Get("job")
		t := template.New("job_logs")
		t, _ = t.Parse(jobLogsPage)
		data := make(map[string]interface{})
		data["jobName"] = job
		data["found"] = false
		for _, j := range exporter.jobs {
			if job == j.Name {
				data["found"] = true
				data["logs"] = j.Logger.GetHistory()
			}
		}
		t.Execute(w, data)
	})

	http.Handle("/job_test", http.TimeoutHandler(
		http.HandlerFunc(jobTestPageHandler),
		time.Second*60,
		"Your request is too long",
	))

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t := template.New("index")
		t, _ = t.Parse(indexPage)
		data := make(map[string]interface{})
		data["metricsPath"] = *metricsPath
		data["jobs"] = exporter.jobs
		t.Execute(w, data)
	})

	level.Info(logger).Log("msg", "Listening", "listenAddress", *listenAddress)
	if err := http.ListenAndServe(*listenAddress, nil); err != nil {
		level.Error(logger).Log("msg", "Error starting HTTP server:", "err", err)
		os.Exit(1)
	}
}
