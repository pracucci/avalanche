package metrics

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// queries map with cardinality as key aa well as value should be cardinality
//var qmap = map[int]string{10: "max_over_time(count({series_id=~\"[0-9]{1,1}\", __name__ =~\"avalanche_metric_mmmmm_._I\",C})[S])", 100: "max_over_time(count({series_id=~\"[0-9]{1,2}\", __name__ =~\"avalanche_metric_mmmmm_._I\",C})[S])", 1000: "max_over_time(count({series_id=~\"[0-9]{1,3}\", __name__ =~\"avalanche_metric_mmmmm_._I\",C})[S])", 10000: "max_over_time(count({series_id=~\"[0-9]{1,3}\", __name__ =~\"avalanche_metric_mmmmm_._I[0-9]{1,1}\",C})[S])", 100000: "max_over_time(count({series_id=~\"[0-9]{1,3}\", __name__ =~\"avalanche_metric_mmmmm_._I[0-9]{1,2}\",C})[S])", 1000000: "max_over_time(count({series_id=~\"[0-9]{1,3}\", __name__ =~\"avalanche_metric_mmmmm_._I[0-9]{1,3}\",C})[S])"}
var qmap = map[int]string{10: "count({series_id=~\"[0-9]{1,1}\", __name__ =~\"avalanche_metric_mmmmm_._I\",C})", 100: "count({series_id=~\"[0-9]{1,2}\", __name__ =~\"avalanche_metric_mmmmm_._I\",C})", 1000: "count({series_id=~\"[0-9]{1,3}\", __name__ =~\"avalanche_metric_mmmmm_._I\",C})", 10000: "count({series_id=~\"[0-9]{1,3}\", __name__ =~\"avalanche_metric_mmmmm_._I[0-9]{1,1}\",C})", 100000: "count({series_id=~\"[0-9]{1,3}\", __name__ =~\"avalanche_metric_mmmmm_._I[0-9]{1,2}\",C})", 1000000: "count({series_id=~\"[0-9]{1,3}\", __name__ =~\"avalanche_metric_mmmmm_._I[0-9]{1,3}\",C})"}

var (
	tstep = map[string]string{"7200": "10", "86400": "60", "806800": "600", "25920000": "600"}
	tdis  = map[string]float64{"7200": 0.5, "86400": 0.35, "806800": 0.1, "25920000": 0.05}
	cdis  = map[int]int{10: 200, 100: 200, 1000: 50, 10000: 10, 100000: 10, 1000000: 5}

	queryTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "promql_query_total",
			Help: "The total number of query generated",
		},
		[]string{"group"},
	)

	queryAccuracy = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "promql_query_value_match",
			Help: "The total number of query returned expected valuess",
		},
		[]string{"group"},
	)

	queryFailures = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "promql_query_failures",
			Help: "The total number of query failures",
		},
		[]string{"group"},
	)

	queryLatency = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name:       "store_query_request_durations",
			Help:       "Store requests latencies in seconds",
			Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001, 0.999: 0.0001},
		},
		[]string{"group"},
	)
)

type ConfigRead struct {
	URL             url.URL
	RequestInterval time.Duration
	Size            int
	RequestCount    int
	Tenant          string
	ConstLabels     []string
	MaxCardinality  int
	HttpBearerToken string
}

// Client for the remote write requests.
type ReadClient struct {
	client  *http.Client
	timeout time.Duration
	config  ConfigRead
}
type Response struct {
	Status string
	Data   *data
}
type data struct {
	ResultType string
	Result     []*series
}
type series struct {
	Metric map[string]string
	Values  [][2]interface{}
}

func init() {
	prometheus.MustRegister(queryLatency)
}

// Make size queries after every RequestInterval RequestCount times.
func Query(config ConfigRead) {
	// N*M means M queries with N cardinality
	var rt http.RoundTripper = &http.Transport{}
	rt = &cortexTenantRoundTripper{tenant: config.Tenant, rt: rt}
	httpClient := &http.Client{Transport: rt}
	c := ReadClient{
		client:  httpClient,
		timeout: time.Minute,
		config:  config,
	}

	for i, label := range config.ConstLabels {
		lkv := strings.Split(label, "=")
		config.ConstLabels[i] = fmt.Sprintf("%s=\"%s\"", lkv[0], lkv[1])
	}

	ticker := time.NewTicker(config.RequestInterval)
	quit := make(chan struct{})

	for {
		select {
		case <-ticker.C:
			runQueryBatch(config, c)
		case <-quit:
			ticker.Stop()
			return
		}
	}
	close(quit)

}

func runQueryBatch(config ConfigRead, c ReadClient) {

	var wg sync.WaitGroup
	queries := generateQueries(config.Size, config.ConstLabels, config.MaxCardinality)

	for q, group := range queries {

		queryTotal.WithLabelValues(group).Inc()
		wg.Add(1)
		go func(q string, group string, c ReadClient) {
			defer wg.Done()
			timer := prometheus.NewTimer(queryLatency.WithLabelValues(group))
			defer timer.ObserveDuration()

			scope := strings.Split(group, ":")

			timeInSecs, _ := strconv.ParseInt(scope[1], 10, 64)
			step, _ := strconv.ParseInt(scope[2], 10, 64)

			bytes := do(q, timeInSecs, step, c)
			var data Response
			json.Unmarshal(bytes, &data)

			expectedValue := scope[0]

			//fmt.Printf("%s , %d, %d", q,timeInSecs, step) 
			if data.Data != nil && len(data.Data.Result) > 0 && len(data.Data.Result[0].Values) > 0 {
				//fmt.Printf("%s gave %s, expected %s \n", q, data.Data.Result[0].Values[0][1], expectedValue)
				numOfPoints := len(data.Data.Result[0].Values)
				if data.Data.Result[0].Values[numOfPoints - 1][1] == expectedValue {
					queryAccuracy.WithLabelValues(group).Inc()
				}
			} else {
				queryFailures.WithLabelValues(group).Inc()
			}
		}(q, group, c)

		wg.Wait()
	}
}

// Make a HTTP Get query and return result
func do(query string, timeInterval int64, step int64, c ReadClient) []byte {

	u := c.config.URL
	q := u.Query()
	q.Set("query", query)
	endTime := time.Now().Unix()
	q.Set("end", strconv.FormatInt(endTime, 10))
	q.Set("start", strconv.FormatInt(endTime - timeInterval, 10))
	q.Set("step", strconv.FormatInt(step, 10))
	//u.Path = "api/v1/query_range"

	u.RawQuery = q.Encode()

	//fmt.Println(u.String())
	var http_bearer_token = c.config.HttpBearerToken
	req, err := http.NewRequest("GET", u.String(), nil)

	if http_bearer_token != " " {
		var bearer = "Bearer " + http_bearer_token
		req.Header.Add("Authorization", bearer)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		fmt.Print(err)
		return nil
	}
	defer resp.Body.Close()
	bodyBytes, err := ioutil.ReadAll(resp.Body)
	return bodyBytes
}

// Generate query map of given size with query is  key and value is cardinality:timeRange:step
func generateQueries(size int, labels []string, maxCardinality int) map[string]string {
	log.Printf("Generating %d queries \n", size)
	timestep := tstep
	total := 0
	timestep = tstep

	s := rand.NewSource(time.Now().UnixNano())
	r := rand.New(s)
	list := make(map[string]string)
	for t, s := range timestep {
		for k, v := range cdis {
			if k > maxCardinality {
				continue
			}
			q := qmap[k]
			q = strings.Replace(q, "T", t, 1)
			q = strings.Replace(q, "S", s, 1)
			q = strings.Replace(q, "C", strings.Join(labels, ","), 1)
			num := int(math.Max(1.0, (float64)(v*size/475.0)*tdis[t]))
			//fmt.Printf("\n\n%d:%s:%s\n", k, t, s)
			//fmt.Printf("%d %d %d %f", num, v*size, v*size /475, (float64)(v*size/475)*tdis[t])
			for i := 0; i < num; i++ {
				ind := r.Intn(num) + 1
				query := strings.Replace(q, "I", strconv.Itoa(ind), 1)
				list[query] = fmt.Sprintf("%d:%s:%s", k, t, s)
				//fmt.Println(query)
			}
			total += num
		}
	}
	return list
}
