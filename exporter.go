package main

import (
	"sync"

	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
)

// Exporter collects SQL metrics. It implements prometheus.Collector.
type Exporter struct {
	jobs   []*Job
	logger RotationLogger
}

// NewExporter returns a new SQL Exporter for the provided config.
func NewExporter(logger RotationLogger, configFile string) (*Exporter, error) {
	if configFile == "" {
		configFile = "config.yml"
	}

	// read config
	cfg, err := Read(configFile)
	if err != nil {
		return nil, err
	}

	exp := &Exporter{
		jobs:   make([]*Job, 0, len(cfg.Jobs)),
		logger: logger,
	}

	// dispatch all jobs
	for _, job := range cfg.Jobs {
		if job == nil {
			continue
		}
		if err := job.Init(logger, cfg.Queries); err != nil {
			level.Warn(logger).Log("msg", "Skipping job. Failed to initialize", "err", err, "job", job.Name)
			continue
		}
		exp.jobs = append(exp.jobs, job)
		job.Prepare()
	}

	return exp, nil
}

// Run runs the jobs
func (e *Exporter) Run() {
	// run all jobs
	for _, job := range e.jobs {
		if job == nil {
			continue
		}
		go job.Run()
	}
}

// RunOnce runs the jobs once
// func (e *Exporter) RunOnce(wg *sync.WaitGroup) {
func (e *Exporter) RunOnce() {
	// run all jobs
	var wg sync.WaitGroup
	wg.Add(len(e.jobs))
	for _, job := range e.jobs {
		go job.RunOnce(&wg)
	}
	wg.Wait()
}

// Describe implements prometheus.Collector
func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	for _, job := range e.jobs {
		if job == nil {
			continue
		}
		for _, query := range job.Queries {
			if query == nil {
				continue
			}
			if query.desc == nil {
				level.Error(e.logger).Log("msg", "Query has no descriptor", "query", query.Name)
				continue
			}
			ch <- query.desc
			ch <- query.errDesc
		}
	}
}

// Collect implements prometheus.Collector
func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	for _, job := range e.jobs {
		if job == nil {
			continue
		}
		for _, query := range job.Queries {
			if query == nil {
				continue
			}
			for _, metrics := range query.metrics {
				for _, metric := range metrics {
					ch <- metric
				}
			}
			ch <- prometheus.MustNewConstMetric(
				query.errDesc,
				prometheus.CounterValue,
				float64(query.log.GerErrorsCount()),
			)
		}
	}
}
