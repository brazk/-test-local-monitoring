package exporter

import (
	"io/ioutil"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/prometheus/client_golang/prometheus"
)

// Read attempts to parse the given config and return a file
// object
func Read(path string) (File, error) {
	f := File{}

	fh, err := os.Open(path)
	if err != nil {
		return f, err
	}
	defer fh.Close()

	buf, err := ioutil.ReadAll(fh)
	if err != nil {
		return f, err
	}

	if err := yaml.Unmarshal(buf, &f); err != nil {
		return f, err
	}
	return f, nil
}

// File is a collection of jobs
type File struct {
	Jobs    []*Job            `yaml:"jobs"`
	Queries map[string]string `yaml:"queries,omitempty"`
}

// Job is a collection of connections and queries
type Job struct {
	Logger          *RotationLogger `yaml:"-"` // Logger for collecting job-level logs (connections problems, etc.)
	conns           []*connection
	Name            string        `yaml:"name"`                // name of this job
	KeepAlive       bool          `yaml:"keepalive,omitempty"` // keep connection between runs?
	Interval        time.Duration `yaml:"interval"`            // interval at which this job is run
	ConnMaxLifetime time.Duration `yaml:"conn_max_lifetime"`   // interval at which this job is run
	Connections     []string      `yaml:"connections"`
	Queries         []*Query      `yaml:"queries"`
	StartupSQL      []string      `yaml:"startup_sql,omitempty"` // SQL executed on startup
	tracer          *opentracing.Tracer
}

type connection struct {
	conn     *sqlx.DB
	url      *url.URL
	driver   string
	host     string
	database string
	user     string
}

// Query is an SQL query that is executed on a connection
type Query struct {
	sync.Mutex `yaml:"-"`
	Logger     *RotationLogger `yaml:"-"` //Logger for collectiong query-level logs (invalid queries, etc.)
	desc       *prometheus.Desc
	errDesc    *prometheus.Desc
	Durations  prometheus.Summary
	metrics    map[*connection][]prometheus.Metric
	Name       string   `yaml:"name"`                // the prometheus metric name
	Help       string   `yaml:"help"`                // the prometheus metric help text
	Labels     []string `yaml:"labels,omitempty"`    // expose these columns as labels per gauge
	Values     []string `yaml:"values"`              // expose each of these as an gauge
	Query      string   `yaml:"query,flow"`          // a literal query
	QueryRef   string   `yaml:"query_ref,omitempty"` // references an query in the query map
}
