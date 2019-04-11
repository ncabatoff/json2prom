package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type (
	Indexer struct {
		Name     string
		Label    string
		Key      string
		Metrics  []Metric
		Contains []Indexer
	}

	Metric struct {
		Labels []string
		Value  string
	}

	Builder struct {
		names  []string
		labels map[string]string
	}
)

func (b Builder) Clone() Builder {
	newb := Builder{
		names:  make([]string, len(b.names)),
		labels: make(map[string]string, len(b.labels)),
	}
	for i, n := range b.names {
		newb.names[i] = n
	}
	for k, v := range b.labels {
		newb.labels[k] = v
	}
	return newb
}

func (ind Indexer) KeyOrName() string {
	if ind.Key == "" {
		return ind.Name
	}
	return ind.Key
}

func getConfig(content string) ([]Indexer, error) {
	var y []Indexer
	err := yaml.Unmarshal([]byte(content), &y)
	if err != nil {
		return nil, err
	}

	if len(y) == 0 {
		return nil, fmt.Errorf("empty config")
	}

	return y, nil
}

func main() {
	cfg := `
- name: vault_replication_status
  key: data
  contains:
  - label: replicationType
    key: '*'
    metrics:
    - value: last_reindex_epoch
      labels: [mode]
    - value: last_wal
      labels: [mode]
    - value: last_remote_wal
      labels: [mode]
    - labels: [mode, cluster_id, state, known_secondaries, primary_cluster_addr, known_primary_cluster_addrs]
`
	indexers, err := getConfig(cfg)
	if err != nil {
		log.Fatal(err)
	}

	// log.Printf("%#v\n", indexers)
	// Key:"data", Contains:
	//   Label:"replicationType", Key:"*", Metrics:
	//     Value:"last_reindex_epoch"
	//     Value:"last_wal"

	in, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		log.Fatal(err)
	}

	var j interface{}
	err = json.Unmarshal(in, &j)
	if err != nil {
		log.Fatal(err)
	}

	doj(Builder{}, indexers, j)
}

func doj(b Builder, indexers []Indexer, j interface{}) {
	if len(indexers) == 0 {
		return
	}
	switch v := j.(type) {
	case []interface{}:
		for i := range v {
			if m, ok := v[i].(map[string]interface{}); ok {
				object(b.Clone(), indexers, m)
			} else {
				log.Fatal("if given an array, it must be an array of objects")
			}
		}
	case map[string]interface{}:
		object(b.Clone(), indexers, v)
	default:
		log.Fatal("only accept objects and arrays of objects")
	}
}

func emit(b Builder, v float64) {
	name := strings.Join(b.names, "_")
	var labels []string
	for k, v := range b.labels {
		labels = append(labels, fmt.Sprintf(`%s="%s"`, k, v))
	}
	sort.Strings(labels)

	fmt.Printf("%s{%s} %f\n", name, strings.Join(labels, ","), v)
}

func dom(b Builder, metrics []Metric, j map[string]interface{}) {
	for _, m := range metrics {
		b := b.Clone()

		var foundLabels bool
		for _, lab := range m.Labels {
			if j[lab] != nil {
				// TODO: should we abort if any labels missing?  That would help
				// ensure we don't have duplicate metrics.  But it seems rough
				// to force user/source to always be consistent.
				b.labels[lab] = fmt.Sprintf("%s", j[lab])
				foundLabels = true
			}
		}

		if m.Value == "" && !foundLabels {
			continue
		}

		if m.Value != "" {
			b.names = append(b.names, m.Value)
		}

		value := 1.0
		if m.Value != "" {
			switch v := j[m.Value].(type) {
			case float64:
				value = v
			case string:
				f, err := strconv.ParseFloat(v, 64)
				if err != nil {
					value = math.NaN()
				} else {
					value = f
				}
			case nil:
				continue
			default:
				value = math.NaN()
			}
		}

		emit(b, value)
	}
}

func object(b Builder, indexers []Indexer, m map[string]interface{}) {
	for _, ind := range indexers {
		if ind.Name != "" {
			b.names = append(b.names, ind.Name)
		}
		if ind.Key == "*" {
			for k, v := range m {
				if ind.Label != "" {
					b2 := b.Clone()
					b2.labels[ind.Label] = k
					doj(b2, ind.Contains, v)
					if dict, ok := v.(map[string]interface{}); ok {
						dom(b2, ind.Metrics, dict)
					}
				}
			}
		}
		if indexed, ok := m[ind.KeyOrName()]; ok {
			doj(b.Clone(), ind.Contains, indexed)
		}
	}
}
