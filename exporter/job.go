package exporter

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/cenkalti/backoff"
	_ "github.com/denisenkom/go-mssqldb" // register the MS-SQL driver
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	_ "github.com/go-sql-driver/mysql" // register the MySQL driver
	"github.com/jmoiron/sqlx"
	_ "github.com/kshvakov/clickhouse" // register the ClickHouse driver
	_ "github.com/lib/pq"              // register the PostgreSQL driver
	"github.com/prometheus/client_golang/prometheus"
)

var (
	// MetricNameRE matches any invalid metric name
	// characters, see github.com/prometheus/common/model.MetricNameRE
	MetricNameRE = regexp.MustCompile("[^a-zA-Z0-9_:]+")
)

// Init will initialize the metric descriptors
func (j *Job) Init(tracer opentracing.Tracer, logger *RotationLogger, queries map[string]string) error {
	if tracer == nil {
		// No tracing found, use noop one.
		tracer = &opentracing.NoopTracer{}
	}

	j.tracer = &tracer
	j.Logger = newRotationLogger(logger, logger.maxMessages)
	j.Logger.SetLogger(log.With(j.Logger.GetLogger(), "job", j.Name))
	// register each query as an metric
	for _, q := range j.Queries {
		if q == nil {
			level.Warn(j.Logger).Log("msg", "Skipping invalid query")
			continue
		}
		q.Logger = newRotationLogger(j.Logger, logger.maxMessages)
		q.Logger.SetLogger(log.With(q.Logger.GetLogger(), "query", q.Name))
		if q.Query == "" && q.QueryRef != "" {
			if qry, found := queries[q.QueryRef]; found {
				q.Query = qry
			}
		}
		if q.Query == "" {
			level.Warn(q.Logger).Log("msg", "Skipping empty query")
			continue
		}
		if q.metrics == nil {
			// we have no way of knowing how many metrics will be returned by the
			// queries, so we just assume that each query returns at least one metric.
			// after the each round of collection this will be resized as necessary.
			q.metrics = make(map[*connection][]prometheus.Metric, len(j.Queries))
		}
		// try to satisfy prometheus naming restrictions
		name := MetricNameRE.ReplaceAllString("sql_"+q.Name, "")
		help := q.Help
		// prepare a new metrics descriptor
		//
		// the tricky part here is that the *order* of labels has to match the
		// order of label values supplied to NewConstMetric later
		q.desc = prometheus.NewDesc(
			name,
			help,
			append(q.Labels, "driver", "host", "database", "user", "col"),
			prometheus.Labels{
				"sql_job": j.Name,
			},
		)
		q.errDesc = prometheus.NewDesc(
			"sql_query_errors",
			"Query errors",
			nil,
			prometheus.Labels{
				"sql_job":   j.Name,
				"sql_query": q.Name,
			},
		)
		q.Durations = prometheus.NewSummary(prometheus.SummaryOpts{
			Name:       "sql_query_durations",
			Help:       "SQL query durations.",
			Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
			ConstLabels: prometheus.Labels{
				"sql_job":   j.Name,
				"sql_query": q.Name,
			},
		})
		prometheus.MustRegister(q.Durations)
	}
	return nil
}

// Prepare the job
func (j *Job) Prepare() {
	if j.Logger == nil {
		j.Logger = newRotationLogger(log.NewNopLogger(), 100)
	}
	// if there are no connection URLs for this job it can't be run
	if j.Connections == nil {
		level.Error(j.Logger).Log("msg", "No conenctions for job", "job", j.Name)
		return
	}
	// make space for the connection objects
	if j.conns == nil {
		j.conns = make([]*connection, 0, len(j.Connections))
	}
	// parse the connection URLs and create an connection object for each
	if len(j.conns) < len(j.Connections) {
		for _, conn := range j.Connections {
			u, err := url.Parse(conn)
			if err != nil {
				level.Error(j.Logger).Log("msg", "Failed to parse URL", "url", conn, "err", err)
				continue
			}
			user := ""
			if u.User != nil {
				user = u.User.Username()
			}
			// we expose some of the connection variables as labels, so we need to
			// remember them
			j.conns = append(j.conns, &connection{
				conn:     nil,
				url:      u,
				driver:   u.Scheme,
				host:     u.Host,
				database: strings.TrimPrefix(u.Path, "/"),
				user:     user,
			})
		}
	}
}

// Run the job
func (j *Job) Run() {
	level.Debug(j.Logger).Log("msg", "Starting")
	// enter the run loop
	// tries to run each query on each connection at approx the interval
	for {
		bo := backoff.NewExponentialBackOff()
		bo.MaxElapsedTime = j.Interval
		if err := backoff.Retry(j.runOnce, bo); err != nil {
			level.Error(j.Logger).Log("msg", "Failed to run", "err", err)
		}
		level.Debug(j.Logger).Log("msg", "Sleeping until next run", "sleep", j.Interval.String())
		time.Sleep(j.Interval)
	}
}

// RunOnce run the job once
func (j *Job) RunOnce() {
	if err := j.runOnce(); err != nil {
		level.Error(j.Logger).Log("msg", "Failed to run", "err", err)
	}
}

func (j *Job) runOnceConnection(conn *connection, done chan int) {
	span := (*j.tracer).StartSpan("job.runOnceConnection")
	span.SetTag("job.name", j.Name)
	updated := 0
	defer func() {
		done <- updated
		span.Finish()
	}()

	// connect to DB if not connected already
	if err := conn.connect(j); err != nil {
		level.Warn(j.Logger).Log("msg", "Failed to connect", "err", err)
		return
	}

	ctx := ContextWithTracer(opentracing.ContextWithSpan(context.Background(), span), *j.tracer)
	for _, q := range j.Queries {
		if q == nil {
			continue
		}
		if q.desc == nil {
			// this may happen if the metric registration failed
			level.Warn(q.Logger).Log("msg", "Skipping query. Collector is nil")
			continue
		}
		level.Debug(q.Logger).Log("msg", "Running Query")
		// execute the query on the connection
		if err := q.Run(ctx, conn); err != nil {
			level.Warn(q.Logger).Log("msg", "Failed to run query", "err", err)
			continue
		}
		level.Debug(q.Logger).Log("msg", "Query finished")
		updated++
	}
}

func (j *Job) runOnce() error {
	doneChan := make(chan int, len(j.conns))

	// execute queries for each connection in parallel
	for _, conn := range j.conns {
		go j.runOnceConnection(conn, doneChan)
	}

	// connections now run in parallel, wait for and collect results
	updated := 0
	for range j.conns {
		updated += <-doneChan
	}

	if updated < 1 {
		return fmt.Errorf("zero queries ran")
	}
	return nil
}

func (c *connection) connect(job *Job) error {
	// already connected
	if c.conn != nil {
		return nil
	}
	dsn := c.url.String()
	switch c.url.Scheme {
	case "mysql":
		dsn = strings.TrimPrefix(dsn, "mysql://")
	case "clickhouse":
		dsn = "tcp://" + strings.TrimPrefix(dsn, "clickhouse://")
	}
	conn, err := sqlx.Connect(c.url.Scheme, dsn)
	if err != nil {
		return err
	}

	connMaxLifetime := job.Interval * 2
	if job.ConnMaxLifetime != 0 {
		connMaxLifetime = job.ConnMaxLifetime
	}

	// be nice and don't use up too many connections for mere metrics
	conn.SetMaxOpenConns(1)
	conn.SetMaxIdleConns(1)
	conn.SetConnMaxLifetime(connMaxLifetime)

	// execute StartupSQL
	for _, query := range job.StartupSQL {
		level.Debug(job.Logger).Log("msg", "StartupSQL", "Query:", query)
		conn.MustExec(query)
	}

	c.conn = conn
	return nil
}
