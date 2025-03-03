package main

import (
	"fmt"
	"math"
	"math/rand"
	"strconv"
	"strings"
	"time"
)

// queries map with cardinality as key aa well as value should be cardinality
//var qmap = map[int]string{10: "max_over_time(count({series_id=~\"[0-9]{1,1}\", __name__ =~\"avalanche_metric_mmmmm_._I\",C})[T:S])", 100: "max_over_time(count({series_id=~\"[0-9]{1,2}\", __name__ =~\"avalanche_metric_mmmmm_._I\",C})[T:S])", 1000: "max_over_time(count({series_id=~\"[0-9]{1,3}\", __name__ =~\"avalanche_metric_mmmmm_._I\",C})[T:S])", 10000: "max_over_time(count({series_id=~\"[0-9]{1,3}\", __name__ =~\"avalanche_metric_mmmmm_._I[0-9]{1,1}\",C})[T:S])", 100000: "max_over_time(count({series_id=~\"[0-9]{1,3}\", __name__ =~\"avalanche_metric_mmmmm_._I[0-9]{1,2}\",C})[T:S])", 1000000: "max_over_time(count({series_id=~\"[0-9]{1,3}\", __name__ =~\"avalanche_metric_mmmmm_._I[0-9]{1,3}\",C})[T:S])"}

var rmap = map[int]string{1: "max_over_time(count({series_id=~\"0\", __name__ =~\"avalanche_metric_mmmmm_._I\"})[T:S])",10: "max_over_time(count({series_id=~\"[0-9]{1,1}\", __name__ =~\"avalanche_metric_mmmmm_._I\"})[T:S])", 100: "max_over_time(count({series_id=~\"[0-9]{1,2}\", __name__ =~\"avalanche_metric_mmmmm_._I\"})[T:S])", 1000: "max_over_time(count({series_id=~\"[0-9]{1,3}\", __name__ =~\"avalanche_metric_mmmmm_._I\"})[T:S])", 10000: "max_over_time(count({series_id=~\"[0-9]{1,3}\", __name__ =~\"avalanche_metric_mmmmm_._I[0-9]{1,1}\"})[T:S])", 100000: "max_over_time(count({series_id=~\"[0-9]{1,3}\", __name__ =~\"avalanche_metric_mmmmm_._I[0-9]{1,2}\"})[T:S])", 1000000: "max_over_time(count({series_id=~\"[0-9]{1,3}\", __name__ =~\"avalanche_metric_mmmmm_._I[0-9]{1,3}\"})[T:S])"}
var (
	tStep = map[string]string{"30m": "30s", "2h": "30s", "24h": "1m", "7d": "10m", "30d": "10m"}
	tDis  = map[string]float64{"30m": 0.9, "2h": 0.08, "24h": 0.02, "30d": 0.001}
	cDis  = map[int]int{1: 200, 10: 200, 100: 50, 1000: 10, 10000: 10, 100000: 5}

)

func main() {
	generateRuleQueries(3300, 4000001)
}
// Generate query map of given size with query is  key and value is cardinality:timeRange:step
func generateRuleQueries(size int, maxCardinality int) {
	//log.Printf("Generating queries \n")
	fmt.Printf("groups:\n")
	timestep := tStep
	total := 0

	s := rand.NewSource(time.Now().UnixNano())
	r := rand.New(s)
	list := make(map[string]string)
	for t, s := range timestep {
		for k, v := range cDis {
			if 40*k > maxCardinality {
				continue
			}
			q := rmap[k]
			q = strings.Replace(q, "T", t, 1)
			q = strings.Replace(q, "S", s, 1)
			num := int(math.Max(1.0, (float64)(v*size/475)*tDis[t]))
			//fmt.Printf("\n- name: %d:%s:%s:%d", 40*k, t, s, i%100)
			for i := 0; i < num; i++ {
				if i %100 == 0 {
					fmt.Printf("\n- name: %d:%s:%s:%d", 40*k, t, s, i/100)
					fmt.Printf("\n  rules: \n")
				}
				// if k > 1000, I (metric number) can have resticted vlues. i.e  from 0 to v/1000 -1
				merticCardinality := 1000
				if k > 1000 {
					merticCardinality = int(math.Min( 1000, (float64)(k/1000)))
				}
				ind := r.Intn(merticCardinality -1) + 1
				query := strings.Replace(q, "I", strconv.Itoa(ind), 1)
				list[query] = fmt.Sprintf("%d:%s:%s", k, t, s)
				fmt.Printf("\n  - record: record:%d", total+i)
				fmt.Printf("\n    expr: %s", query)
			}
			total += num
		}
	}
}
