package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"text/template"
	"time"
)

type IntStats struct {
	Rx_packets          float64 `json:"rx_packets"`
	Tx_packets          float64 `json:"tx_packets"`
	Rx_bytes            float64 `json:"rx_bytes"`
	Tx_bytes            float64 `json:"tx_bytes"`
	Rx_errors           float64 `json:"rx_errors"`
	Tx_errors           float64 `json:"tx_errors"`
	Rx_dropped          float64 `json:"rx_dropped"`
	Tx_dropped          float64 `json:"tx_dropped"`
	Multicast           float64 `json:"multicast"`
	Collisions          float64 `json:"collisions"`
	Rx_length_errors    float64 `json:"rx_length_errors"`
	Rx_over_errors      float64 `json:"rx_over_errors"`
	Rx_crc_errors       float64 `json:"rx_crc_errors"`
	Rx_frame_errors     float64 `json:"rx_frame_errors"`
	Rx_fifo_errors      float64 `json:"rx_fifo_errors"`
	Rx_missed_errors    float64 `json:"rx_missed_errors"`
	Tx_aborted_errors   float64 `json:"tx_aborted_errors"`
	Tx_carrier_errors   float64 `json:"tx_carrier_errors"`
	Tx_fifo_errors      float64 `json:"tx_fifo_errors"`
	Tx_heartbeat_errors float64 `json:"tx_heartbeat_errors"`
}

type IfaceStats struct {
	IfaceStats map[string]IntStats
}

type KernelStats struct {
	Kernel map[string]IfaceStats `json:"kernel"`
}

type bulkLineIntStats struct {
	Timestamp           int64   `json:"timestamp"`
	Hostname            string  `json:"hostname"`
	Iface               string  `json:"iface"`
	Rx_packets          float64 `json:"rx_packets"`
	Tx_packets          float64 `json:"tx_packets"`
	Rx_bytes            float64 `json:"rx_bytes"`
	Tx_bytes            float64 `json:"tx_bytes"`
	Rx_bits             float64 `json:"rx_bits"`
	Tx_bits             float64 `json:"tx_bits"`
	Rx_errors           float64 `json:"rx_errors"`
	Tx_errors           float64 `json:"tx_errors"`
	Rx_dropped          float64 `json:"rx_dropped"`
	Tx_dropped          float64 `json:"tx_dropped"`
	Multicast           float64 `json:"multicast"`
	Collisions          float64 `json:"collisions"`
	Rx_length_errors    float64 `json:"rx_length_errors"`
	Rx_over_errors      float64 `json:"rx_over_errors"`
	Rx_crc_errors       float64 `json:"rx_crc_errors"`
	Rx_frame_errors     float64 `json:"rx_frame_errors"`
	Rx_fifo_errors      float64 `json:"rx_fifo_errors"`
	Rx_missed_errors    float64 `json:"rx_missed_errors"`
	Tx_aborted_errors   float64 `json:"tx_aborted_errors"`
	Tx_carrier_errors   float64 `json:"tx_carrier_errors"`
	Tx_fifo_errors      float64 `json:"tx_fifo_errors"`
	Tx_heartbeat_errors float64 `json:"tx_heartbeat_errors"`
}

func main() {
	flag.Usage = func() {
		fmt.Printf("Usage:\n ifstatspy [options] \nOptions:\n")
		flag.PrintDefaults()
	}
	url := flag.String("u", "http://127.0.0.1:9200/", "Url of Elastic cluster api")
	elasticIndex := flag.String("x", "ifaces-stats-", "Index on which we must send in Elastic. Default : 'ifaces-stats-' and will add date automatically")
	netns := flag.String("n", "", "Namespace on which the interfaces should be retrieved")
	ifaces := flag.String("i", "", "List of interfaces that must be monitored (for example : 'eth0,eth1')\n By default, all the interfaces of the namespace are monitored. \n")
	binary := flag.String("b", "ifstat", "For now, 'ifstat' binary is used to retrieve interface stats. \nYou can specify the path of the binary if you don't want to use the \ndefault binary of your system.")
	sendInterval := flag.Int("s", 300, "Interval at which bulk requests should be sent to elastic \nDefault : 300 seconds")
	retrievalInterval := flag.Int("r", 200, "Interval at which stats should be retrieved from interfaces \nDefault : 200 milliseconds")
	hostname := flag.String("h", "", "Hostname of this sender. Will try to discover it by default")
	flag.Parse()

	fmt.Printf("Options used : %v, %v, %v, %v, %v, %v, %v, %v\n", *url, *elasticIndex, *netns, *ifaces, *binary, *sendInterval, *retrievalInterval, *hostname)

	ifacesList := getIfaces(*ifaces)
	fmt.Printf("Ifaces to monitor : %q\n", ifacesList)

	if *hostname == "" {
		*hostname, _ = os.Hostname()
	}

	*elasticIndex = *elasticIndex + time.Now().Format("2006-01-02")

	*url = *url + *elasticIndex

	ensureIndexAndMapping(*url)

	*url = *url + "/_bulk"

	ifacesMonitoring(ifacesList, *binary, *netns, *retrievalInterval, *sendInterval, *url, *hostname)
}

func getIfaces(ifaces string) []string {
	if ifaces != "" {
		var ifacesList []string
		ifacesList = strings.Split(ifaces, ",")
		return ifacesList
	}
	cmd := "ip address show up | grep mtu | cut -f2 -d ':' | cut -f1 -d '@' | tr -d '\n'"
	cmdOutput, err := exec.Command("bash", "-c", cmd).Output()
	if err != nil {
		log.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(cmdOutput)), " ")
}

func ifacesMonitoring(ifacesList []string, binary string, netns string, retrievalInterval int, sendInterval int, url string, hostname string) {
	//var ifacesStats map[string]interface{}
	//ifacesStats = make(map[string]interface{})

	options := " -a -s -e -z -j -p "
	var ifstat_cmd string
	if netns != "" {
		ifstat_cmd = "ip netns exec " + netns + " " + binary + options
	} else {
		ifstat_cmd = binary + options
	}

	// Calculate time at which we must send to elastic
	timeInterval_t := time.Now()
	timeInterval_t = timeInterval_t.Add(time.Duration(sendInterval) * time.Second)
	timeInterval := timeInterval_t.UnixNano() / 1000000

	// Pre-size the slice that will hold the ifaces datas
	lenSlice := len(ifacesList) * (sendInterval * 1000 / retrievalInterval)
	//batchSlice  := make([]map[string]interface{}, lenSlice)
	batchSlice := make([]bulkLineIntStats, lenSlice)
	indexSlice := 0

	// Loop forever
	for {

		//tNowBefore := time.Now()  // for debug timing

		// Time of the sample
		timestamp := time.Now().UnixNano() / 1000000

		for _, iface := range ifacesList {
			ifstatCmd := ifstat_cmd + iface
			cmdOutput, err := exec.Command("bash", "-c", ifstatCmd).Output()
			if err != nil {
				log.Fatal(err)
			}
			var stats bulkLineIntStats
			stats.UnmarshalJSON(cmdOutput, iface, timestamp, hostname)
			// Appending mess with slice size so we use a static index
			batchSlice[indexSlice] = stats
			indexSlice++
		}
		//timeInterval = timeInterval + retrievalInterval
		//if timeInterval > (sendInterval * 1000) {
		if (timeInterval - timestamp) < 0 {
			//go formatAndsendToElastic(ifacesStats)
			go formatAndsendToElastic(batchSlice, url)
			timeInterval_t := time.Now()
			timeInterval_t = timeInterval_t.Add(time.Duration(sendInterval) * time.Second)
			timeInterval = timeInterval_t.UnixNano() / 1000000
			//batchSlice = make([]map[string]interface{}, lenSlice)
			batchSlice = make([]bulkLineIntStats, lenSlice)
			indexSlice = 0
		}
		//tNowAfter := time.Now() // for debug timing
		//fmt.Println(tNowAfter.Sub(tNowBefore))  // for debug timing
		time.Sleep(time.Duration(retrievalInterval) * time.Millisecond)
	}
}

//func formatAndsendToElastic(ifacesStats map[string]interface{}) {
//func formatAndsendToElastic(ifacesStats []map[string]interface{}, url string) {
func formatAndsendToElastic(ifacesStats []bulkLineIntStats, url string) {
	// bulk-line : {"timestamp": 1590824759143, "iface": "tun0", "rx_packets": 21816, "tx_packets": 13997, "rx_bits": 200634104, "tx_bits": 9345912, "rx_errors": 0, "tx_errors": 0, "rx_dropped": 0, "tx_dropped": 0, "multicast": 0, "collisions": 0, "rx_length_errors": 0, "rx_over_errors": 0, "rx_crc_errors": 0, "rx_frame_errors": 0, "rx_fifo_errors": 0, "rx_missed_errors": 0, "tx_aborted_errors": 0, "tx_carrier_errors": 0, "tx_fifo_errors": 0, "tx_heartbeat_errors": 0}
	var dataToSend []byte
	var jsonIndexStr = []byte(`{"index":{}}`)
	var jsonEndBulkStr = []byte("\n")

	jsonIndexStr = append(jsonEndBulkStr, jsonIndexStr...)
	jsonIndexStr = append(jsonIndexStr, jsonEndBulkStr...)

	for _, ifaceStats := range ifacesStats {
		jsonIfaceStats, err := json.Marshal(ifaceStats)
		if err != nil {
			fmt.Println(err)
		}
		//fmt.Println(string(dataToSend))
		bulkLine := append(jsonIndexStr, jsonIfaceStats...)
		dataToSend = append(dataToSend, bulkLine...)
	}

	dataToSend = append(dataToSend, jsonEndBulkStr...)
	//fmt.Println(string(dataToSend))

	sendRequest(url, dataToSend, "POST")

	//response, err := http.Post(url, "application/json", bytes.NewBuffer(dataToSend))
	//if err != nil {
	//  fmt.Println(err)
	//}
	//defer response.Body.Close()

	//body, err := ioutil.ReadAll(response.Body)
	//if err != nil {
	//  fmt.Println(err)
	//}
	//fmt.Println(string(body))
}

func (stats *bulkLineIntStats) UnmarshalJSON(b []byte, iface string, timestamp int64, hostname string) error {
	// We must unmarshal data to get relevant infos... Not very practical compared to python here
	var out map[string]interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		log.Fatal(err)
	}

	ifaceStats := out["kernel"].(map[string]interface{})
	statsOnly := ifaceStats[iface].(map[string]interface{})
	stats.Rx_packets = statsOnly["rx_packets"].(float64)
	stats.Tx_packets = statsOnly["tx_packets"].(float64)
	stats.Rx_bytes = statsOnly["rx_bytes"].(float64)
	stats.Tx_bytes = statsOnly["tx_bytes"].(float64)
	stats.Rx_bits = stats.Rx_bytes * 8
	stats.Tx_bits = stats.Tx_bytes * 8
	stats.Rx_errors = statsOnly["rx_errors"].(float64)
	stats.Tx_errors = statsOnly["tx_errors"].(float64)
	stats.Rx_dropped = statsOnly["rx_dropped"].(float64)
	stats.Tx_dropped = statsOnly["tx_dropped"].(float64)
	stats.Multicast = statsOnly["multicast"].(float64)
	stats.Collisions = statsOnly["collisions"].(float64)
	stats.Rx_length_errors = statsOnly["rx_length_errors"].(float64)
	stats.Rx_over_errors = statsOnly["rx_over_errors"].(float64)
	stats.Rx_crc_errors = statsOnly["rx_crc_errors"].(float64)
	stats.Rx_frame_errors = statsOnly["rx_frame_errors"].(float64)
	stats.Rx_fifo_errors = statsOnly["rx_fifo_errors"].(float64)
	stats.Rx_missed_errors = statsOnly["rx_missed_errors"].(float64)
	stats.Tx_aborted_errors = statsOnly["tx_aborted_errors"].(float64)
	stats.Tx_carrier_errors = statsOnly["tx_carrier_errors"].(float64)
	stats.Tx_fifo_errors = statsOnly["tx_fifo_errors"].(float64)
	stats.Tx_heartbeat_errors = statsOnly["tx_heartbeat_errors"].(float64)

	stats.Timestamp = timestamp
	stats.Hostname = hostname
	stats.Iface = iface

	return nil
}

func ensureIndexAndMapping(url string) {

	var indexSettings = []byte(`{"settings": {"number_of_shards": 2, "number_of_replicas": 2}}`)
	var indexMapping = []byte(`
  {
      "properties": {
          "timestamp": {"type": "date", "format": "epoch_millis"},
          "iface": {"type": "keyword"},
          "hostname": {"type": "keyword"},
          "rx_packets": {"type": "long"},
          "tx_packets": {"type": "long"},
          "rx_bits": {"type": "long"},
          "tx_bits": {"type": "long"},
          "rx_errors": {"type": "long"},
          "tx_errors": {"type": "long"},
          "rx_dropped": {"type": "long"},
          "tx_dropped": {"type": "long"},
          "multicast": {"type": "long"},
          "collisions": {"type": "long"},
          "rx_length_errors": {"type": "long"},
          "rx_over_errors": {"type": "long"},
          "rx_crc_errors": {"type": "long"},
          "rx_frame_errors": {"type": "long"},
          "rx_fifo_errors": {"type": "long"},
          "rx_missed_errors": {"type": "long"},
          "tx_aborted_errors": {"type": "long"},
          "tx_carrier_errors": {"type": "long"},
          "tx_fifo_errors": {"type": "long"},
          "tx_heartbeat_errors": {"type": "long"}
      }
  }`)
	//fmt.Println(indexSettings)
	//fmt.Println(indexMapping)
	sendRequest(url, indexSettings, "PUT")
	sendRequest(url+"/_mapping", indexMapping, "PUT")
}

func autoCreationKibanaDashboard(url string) {
	// Not ready yet
	// Change Visualizations to have auto time span

	type DashboardInfos struct {
		KibanaVersion string
		DashId        string
	}

	dash := DashboardInfos{
		"7.6.2",
		"e43413e0-a354-11ea-befe-09f9d53a9377",
	}

	dashTmpl, err := template.New("dashboard").Parse(`
	{
  "version": "{{.KibanaVersion}}",
  "objects": [
    {
      "id": "{{.DashId}}",
      "type": "dashboard",
      "updated_at": "2020-06-19T06:21:56.652Z",
      "version": "WzEwMjMsMTNd",
      "attributes": {
        "title": "rx_packets_TEST2",
        "hits": 0,
        "description": "",
        "panelsJSON": "[{\"version\":\"7.6.2\",\"gridData\":{\"w\":24,\"h\":15,\"x\":0,\"y\":0,\"i\":\"63f301a8-a15e-4ba1-aad2-2bb443c72fb5\"},\"panelIndex\":\"63f301a8-a15e-4ba1-aad2-2bb443c72fb5\",\"embeddableConfig\":{\"title\":\"rx_packets_TEST2\"},\"title\":\"rx_packets_TEST2\",\"panelRefName\":\"panel_0\"}]",
        "optionsJSON": "{\"hidePanelTitles\":false,\"useMargins\":true}",
        "version": 1,
        "timeRestore": false,
        "kibanaSavedObjectMeta": {
          "searchSourceJSON": "{\"query\":{\"query\":\"hostname : \\\"TEST2\\\" \",\"language\":\"kuery\"},\"filter\":[{\"meta\":{\"alias\":null,\"negate\":false,\"disabled\":false,\"type\":\"phrase\",\"key\":\"hostname\",\"params\":{\"query\":\"TEST2\"},\"indexRefName\":\"kibanaSavedObjectMeta.searchSourceJSON.filter[0].meta.index\"},\"query\":{\"match_phrase\":{\"hostname\":\"TEST2\"}},\"$state\":{\"store\":\"appState\"}}]}"
        }
      },
      "references": [
        {
          "name": "kibanaSavedObjectMeta.searchSourceJSON.filter[0].meta.index",
          "type": "index-pattern",
          "id": "9eb1cfc0-b0af-11ea-ab07-8d20dd347cee"
        },
        {
          "name": "panel_0",
          "type": "visualization",
          "id": "bdbb23c0-a354-11ea-befe-09f9d53a9377"
        }
      ],
      "migrationVersion": {
        "dashboard": "7.3.0"
      }
    }
  ]
	}
	`)
	if err != nil {
		panic(err)
	}
	err = dashTmpl.Execute(os.Stdout, dash)
	if err != nil {
		panic(err)
	}

}

func sendRequest(url string, data []byte, method string) {
	client := &http.Client{}
	req, err := http.NewRequest(method, url, bytes.NewBuffer(data))
	if err != nil {
		fmt.Println(err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	response, err := client.Do(req)

	_, err = ioutil.ReadAll(response.Body)
	if err != nil {
		fmt.Println(err)
	}
	response.Body.Close()
	//fmt.Println(string(body))
}
