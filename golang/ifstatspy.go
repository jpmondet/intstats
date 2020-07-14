// TO FIX:
// - Add an option to match interfaces with a pattern (to facilitate usage on switches that have tens of interfaces)
// - What about interfaces being shut ? Or Flapping ? -> Should return only zéro-ed json but must try
// - Randomize sending a lil' bit to prevent synchronization between devices resulting in a burst to the elastic cluster
// - Some type of virtual interfaces seems to report wrongs stats (Po for exple)
// - Maybe add an option to let the program use ntp instead of local time (which is often incorrect due to timezones & stuff <- especially when Elastic cluster is not at the same time)

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
	kibanaUrl := flag.String("k", "http://127.0.0.1:5601/", "Url of Kibana api")
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

	autoCreationKibanaDashboard(*kibanaUrl, *hostname, ifacesList)

	*url = *url + "/_bulk"

	ifacesMonitoring(ifacesList, *binary, *netns, *retrievalInterval, *sendInterval, *url, *hostname)

}

func getIfaces(ifaces string) []string {
	if ifaces != "" {
		var ifacesList []string
		ifacesList = strings.Split(ifaces, ",")
		return ifacesList
	}
	cmd := "ip link show up | grep mtu | cut -f2 -d ':' | cut -f1 -d '@' | tr -d '\n'"
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

func autoCreationKibanaDashboard(url string, hostname_device string, ifacesList []string) {
	// Not ready yet

	type DashboardInfos struct {
		Hostname string
		Ifaces   []string
		Length   int
		Url      string
	}

	dash := DashboardInfos{
		hostname_device,
		ifacesList,
		len(ifacesList) - 1,
		url,
	}

	dashTmpl, err := template.New("dashboard").Parse(`
	{
  "objects": [
    {
			"id": "{{.Hostname}}-{{.Length}}",
			"type": "dashboard",
			"attributes": {
				"title": "ifaces_{{.Hostname}}",
				"panelsJSON": "[{\"version\":\"7.6.2\",\"gridData\":{\"x\":0,\"y\":0,\"w\":24,\"h\":15,\"i\":\"e1202978-862d-416d-bf88-c439a1a50d9d\"},\"panelIndex\":\"e1202978-862d-416d-bf88-c439a1a50d9d\",\"embeddableConfig\":{},\"panelRefName\":\"panel_0\"},{\"version\":\"7.6.2\",\"gridData\":{\"x\":24,\"y\":0,\"w\":24,\"h\":15,\"i\":\"a0ed4765-1a8e-4c11-9219-51f0c458fdb2\"},\"panelIndex\":\"a0ed4765-1a8e-4c11-9219-51f0c458fdb2\",\"embeddableConfig\":{},\"panelRefName\":\"panel_1\"},{\"version\":\"7.6.2\",\"gridData\":{\"x\":0,\"y\":15,\"w\":24,\"h\":15,\"i\":\"65e9ff9e-167a-4b2f-86ee-a2396be38baa\"},\"panelIndex\":\"65e9ff9e-167a-4b2f-86ee-a2396be38baa\",\"embeddableConfig\":{},\"panelRefName\":\"panel_2\"},{\"version\":\"7.6.2\",\"gridData\":{\"x\":24,\"y\":15,\"w\":24,\"h\":15,\"i\":\"c65bf37b-79c4-49de-817c-3544c407c04f\"},\"panelIndex\":\"c65bf37b-79c4-49de-817c-3544c407c04f\",\"embeddableConfig\":{},\"panelRefName\":\"panel_3\"},{\"version\":\"7.6.2\",\"gridData\":{\"x\":24,\"y\":30,\"w\":24,\"h\":15,\"i\":\"8ef35283-e838-46ae-ace8-6b2b7e7480aa\"},\"panelIndex\":\"8ef35283-e838-46ae-ace8-6b2b7e7480aa\",\"embeddableConfig\":{},\"panelRefName\":\"panel_4\"},{\"version\":\"7.6.2\",\"gridData\":{\"x\":0,\"y\":30,\"w\":24,\"h\":15,\"i\":\"8e425bdc-8625-4a53-b5d0-9d3b693b5b83\"},\"panelIndex\":\"8e425bdc-8625-4a53-b5d0-9d3b693b5b83\",\"embeddableConfig\":{},\"panelRefName\":\"panel_5\"}]",
				"optionsJSON": "{\"hidePanelTitles\":false,\"useMargins\":true}",
				"timeRestore": false,
				"kibanaSavedObjectMeta": {
					"searchSourceJSON": "{\"query\":{\"language\":\"kuery\",\"query\":\"hostname : \\\"{{.Hostname}}\\\" \"},\"filter\":[{{ $length := .Length }}{{range $i, $iface := .Ifaces}}{\"$state\":{\"store\":\"globalState\"},\"meta\":{\"alias\":null,\"disabled\":true,\"key\":\"iface\",\"negate\":false,\"params\":{\"query\":\"{{$iface}}\"},\"type\":\"phrase\",\"indexRefName\":\"kibanaSavedObjectMeta.searchSourceJSON.filter[0].meta.index\"},\"query\":{\"match_phrase\":{\"iface\":\"{{$iface}}\"}}}{{if eq $length $i}}{{else}},{{end}}{{end}}]}"
				}
			},
			"references": [
				{
					"name": "kibanaSavedObjectMeta.searchSourceJSON.filter[0].meta.index",
					"type": "index-pattern",
					"id": "9eb1cfc0-b0af-11ea-ab07-8d20dd347cee"
				},
				{
					"name": "kibanaSavedObjectMeta.searchSourceJSON.filter[1].meta.index",
					"type": "index-pattern",
					"id": "9eb1cfc0-b0af-11ea-ab07-8d20dd347cee"
				},
				{
					"name": "panel_0",
					"type": "visualization",
					"id": "8cae2350-b203-11ea-88e4-b9b64ca01b05"
				},
				{
					"name": "panel_1",
					"type": "visualization",
					"id": "f87e8a70-b203-11ea-88e4-b9b64ca01b05"
				},
				{
					"name": "panel_2",
					"type": "visualization",
					"id": "58b61450-b653-11ea-bd24-adcf27ac7621"
				},
				{
					"name": "panel_3",
					"type": "visualization",
					"id": "aa6a9040-b654-11ea-bd24-adcf27ac7621"
				},
				{
					"name": "panel_4",
					"type": "visualization",
					"id": "3f410260-b652-11ea-bd24-adcf27ac7621"
				},
				{
					"name": "panel_5",
					"type": "visualization",
					"id": "fce69ec0-b651-11ea-bd24-adcf27ac7621"
				}
			],
			"migrationVersion": {
				"dashboard": "7.3.0"
			}
		},
		{
			"id": "9eb1cfc0-b0af-11ea-ab07-8d20dd347cee",
			"type": "index-pattern",
			"attributes": {
				"title": "ifaces-stats-*",
				"timeFieldName": "timestamp",
				"fields": "[{\"name\":\"_id\",\"type\":\"string\",\"esTypes\":[\"_id\"],\"count\":0,\"scripted\":false,\"searchable\":true,\"aggregatable\":true,\"readFromDocValues\":false},{\"name\":\"_index\",\"type\":\"string\",\"esTypes\":[\"_index\"],\"count\":0,\"scripted\":false,\"searchable\":true,\"aggregatable\":true,\"readFromDocValues\":false},{\"name\":\"_score\",\"type\":\"number\",\"count\":0,\"scripted\":false,\"searchable\":false,\"aggregatable\":false,\"readFromDocValues\":false},{\"name\":\"_source\",\"type\":\"_source\",\"esTypes\":[\"_source\"],\"count\":0,\"scripted\":false,\"searchable\":false,\"aggregatable\":false,\"readFromDocValues\":false},{\"name\":\"_type\",\"type\":\"string\",\"esTypes\":[\"_type\"],\"count\":0,\"scripted\":false,\"searchable\":true,\"aggregatable\":true,\"readFromDocValues\":false},{\"name\":\"collisions\",\"type\":\"number\",\"esTypes\":[\"long\"],\"count\":0,\"scripted\":false,\"searchable\":true,\"aggregatable\":true,\"readFromDocValues\":true},{\"name\":\"hostname\",\"type\":\"string\",\"esTypes\":[\"keyword\"],\"count\":0,\"scripted\":false,\"searchable\":true,\"aggregatable\":true,\"readFromDocValues\":true},{\"name\":\"iface\",\"type\":\"string\",\"esTypes\":[\"keyword\"],\"count\":0,\"scripted\":false,\"searchable\":true,\"aggregatable\":true,\"readFromDocValues\":true},{\"name\":\"multicast\",\"type\":\"number\",\"esTypes\":[\"long\"],\"count\":0,\"scripted\":false,\"searchable\":true,\"aggregatable\":true,\"readFromDocValues\":true},{\"name\":\"rx_bits\",\"type\":\"number\",\"esTypes\":[\"long\"],\"count\":0,\"scripted\":false,\"searchable\":true,\"aggregatable\":true,\"readFromDocValues\":true},{\"name\":\"rx_bytes\",\"type\":\"number\",\"esTypes\":[\"long\"],\"count\":0,\"scripted\":false,\"searchable\":true,\"aggregatable\":true,\"readFromDocValues\":true},{\"name\":\"rx_crc_errors\",\"type\":\"number\",\"esTypes\":[\"long\"],\"count\":0,\"scripted\":false,\"searchable\":true,\"aggregatable\":true,\"readFromDocValues\":true},{\"name\":\"rx_dropped\",\"type\":\"number\",\"esTypes\":[\"long\"],\"count\":0,\"scripted\":false,\"searchable\":true,\"aggregatable\":true,\"readFromDocValues\":true},{\"name\":\"rx_errors\",\"type\":\"number\",\"esTypes\":[\"long\"],\"count\":0,\"scripted\":false,\"searchable\":true,\"aggregatable\":true,\"readFromDocValues\":true},{\"name\":\"rx_fifo_errors\",\"type\":\"number\",\"esTypes\":[\"long\"],\"count\":0,\"scripted\":false,\"searchable\":true,\"aggregatable\":true,\"readFromDocValues\":true},{\"name\":\"rx_frame_errors\",\"type\":\"number\",\"esTypes\":[\"long\"],\"count\":0,\"scripted\":false,\"searchable\":true,\"aggregatable\":true,\"readFromDocValues\":true},{\"name\":\"rx_length_errors\",\"type\":\"number\",\"esTypes\":[\"long\"],\"count\":0,\"scripted\":false,\"searchable\":true,\"aggregatable\":true,\"readFromDocValues\":true},{\"name\":\"rx_missed_errors\",\"type\":\"number\",\"esTypes\":[\"long\"],\"count\":0,\"scripted\":false,\"searchable\":true,\"aggregatable\":true,\"readFromDocValues\":true},{\"name\":\"rx_over_errors\",\"type\":\"number\",\"esTypes\":[\"long\"],\"count\":0,\"scripted\":false,\"searchable\":true,\"aggregatable\":true,\"readFromDocValues\":true},{\"name\":\"rx_packets\",\"type\":\"number\",\"esTypes\":[\"long\"],\"count\":0,\"scripted\":false,\"searchable\":true,\"aggregatable\":true,\"readFromDocValues\":true},{\"name\":\"timestamp\",\"type\":\"date\",\"esTypes\":[\"date\"],\"count\":0,\"scripted\":false,\"searchable\":true,\"aggregatable\":true,\"readFromDocValues\":true},{\"name\":\"tx_aborted_errors\",\"type\":\"number\",\"esTypes\":[\"long\"],\"count\":0,\"scripted\":false,\"searchable\":true,\"aggregatable\":true,\"readFromDocValues\":true},{\"name\":\"tx_bits\",\"type\":\"number\",\"esTypes\":[\"long\"],\"count\":0,\"scripted\":false,\"searchable\":true,\"aggregatable\":true,\"readFromDocValues\":true},{\"name\":\"tx_bytes\",\"type\":\"number\",\"esTypes\":[\"long\"],\"count\":0,\"scripted\":false,\"searchable\":true,\"aggregatable\":true,\"readFromDocValues\":true},{\"name\":\"tx_carrier_errors\",\"type\":\"number\",\"esTypes\":[\"long\"],\"count\":0,\"scripted\":false,\"searchable\":true,\"aggregatable\":true,\"readFromDocValues\":true},{\"name\":\"tx_dropped\",\"type\":\"number\",\"esTypes\":[\"long\"],\"count\":0,\"scripted\":false,\"searchable\":true,\"aggregatable\":true,\"readFromDocValues\":true},{\"name\":\"tx_errors\",\"type\":\"number\",\"esTypes\":[\"long\"],\"count\":0,\"scripted\":false,\"searchable\":true,\"aggregatable\":true,\"readFromDocValues\":true},{\"name\":\"tx_fifo_errors\",\"type\":\"number\",\"esTypes\":[\"long\"],\"count\":0,\"scripted\":false,\"searchable\":true,\"aggregatable\":true,\"readFromDocValues\":true},{\"name\":\"tx_heartbeat_errors\",\"type\":\"number\",\"esTypes\":[\"long\"],\"count\":0,\"scripted\":false,\"searchable\":true,\"aggregatable\":true,\"readFromDocValues\":true},{\"name\":\"tx_packets\",\"type\":\"number\",\"esTypes\":[\"long\"],\"count\":0,\"scripted\":false,\"searchable\":true,\"aggregatable\":true,\"readFromDocValues\":true}]",
				"fieldFormatMap": "{\"timestamp\":{\"id\":\"date\",\"params\":{\"parsedUrl\":{\"origin\":\"{{.Url}}\",\"pathname\":\"/app/kibana\",\"basePath\":\"\"}}},\"rx_bytes\":{\"id\":\"bytes\",\"params\":{\"parsedUrl\":{\"origin\":\"{{.Url}}\",\"pathname\":\"/app/kibana\",\"basePath\":\"\"}}},\"tx_bytes\":{\"id\":\"bytes\",\"params\":{\"parsedUrl\":{\"origin\":\"{{.Url}}\",\"pathname\":\"/app/kibana\",\"basePath\":\"\"}}}}"
			},
			"references": [],
			"migrationVersion": {
				"index-pattern": "7.6.0"
			}
		},
		{
			"id": "8cae2350-b203-11ea-88e4-b9b64ca01b05",
			"type": "visualization",
			"attributes": {
				"title": "bits",
				"visState": "{\"title\":\"bits\",\"type\":\"metrics\",\"params\":{\"axis_formatter\":\"number\",\"axis_position\":\"left\",\"axis_scale\":\"normal\",\"background_color_rules\":[{\"id\":\"05174ff0-a34b-11ea-aca0-a5b495fa2259\"}],\"bar_color_rules\":[{\"id\":\"07e67580-a34b-11ea-aca0-a5b495fa2259\"}],\"default_index_pattern\":\"ifaces-stats-*\",\"default_timefield\":\"timestamp\",\"gauge_color_rules\":[{\"id\":\"095a29c0-a34b-11ea-aca0-a5b495fa2259\"}],\"gauge_inner_width\":10,\"gauge_style\":\"half\",\"gauge_width\":10,\"id\":\"61ca57f0-469d-11e7-af02-69e470af7417\",\"index_pattern\":\"ifaces-stats-*\",\"interval\":\"auto\",\"isModelInvalid\":false,\"series\":[{\"axis_min\":\"0\",\"axis_position\":\"right\",\"chart_type\":\"line\",\"color\":\"#68BC00\",\"fill\":0.5,\"filter\":{\"language\":\"kuery\",\"query\":\"\"},\"formatter\":\"number\",\"id\":\"61ca57f1-469d-11e7-af02-69e470af7417\",\"label\":\"rx(bits)\",\"line_width\":1,\"metrics\":[{\"field\":\"rx_bits\",\"id\":\"61ca57f2-469d-11e7-af02-69e470af7417\",\"type\":\"avg\"},{\"field\":\"61ca57f2-469d-11e7-af02-69e470af7417\",\"id\":\"f52bd7a0-a34a-11ea-aca0-a5b495fa2259\",\"lag\":\"\",\"type\":\"serial_diff\"}],\"point_size\":1,\"separate_axis\":0,\"split_filters\":[{\"color\":\"#68BC00\",\"filter\":{\"language\":\"kuery\",\"query\":\"\"},\"id\":\"51dba4a0-b1fe-11ea-b7b0-cbda6a4b1cb5\"}],\"split_mode\":\"everything\",\"stacked\":\"none\",\"terms_field\":\"hostname\",\"terms_order_by\":\"_key\",\"terms_size\":\"1000\",\"type\":\"timeseries\"},{\"axis_position\":\"right\",\"chart_type\":\"line\",\"color\":\"rgba(188,0,121,1)\",\"fill\":0.5,\"formatter\":\"number\",\"id\":\"f4386e90-b1fe-11ea-b7b0-cbda6a4b1cb5\",\"label\":\"tx(bits)\",\"line_width\":1,\"metrics\":[{\"field\":\"tx_bits\",\"id\":\"f4386e91-b1fe-11ea-b7b0-cbda6a4b1cb5\",\"type\":\"avg\"},{\"field\":\"f4386e91-b1fe-11ea-b7b0-cbda6a4b1cb5\",\"id\":\"a09a6030-b1ff-11ea-b7b0-cbda6a4b1cb5\",\"lag\":\"\",\"type\":\"serial_diff\"}],\"point_size\":1,\"separate_axis\":0,\"split_mode\":\"everything\",\"stacked\":\"none\",\"type\":\"timeseries\"}],\"show_grid\":1,\"show_legend\":1,\"time_field\":\"timestamp\",\"type\":\"timeseries\"},\"aggs\":[]}",
				"uiStateJSON": "{}",
				"description": "",
				"version": 1,
				"kibanaSavedObjectMeta": {
					"searchSourceJSON": "{}"
				}
			},
			"references": [],
			"migrationVersion": {
				"visualization": "7.4.2"
			}
		},
		{
			"id": "f87e8a70-b203-11ea-88e4-b9b64ca01b05",
			"type": "visualization",
			"attributes": {
				"title": "errors",
				"visState": "{\"title\":\"errors\",\"type\":\"metrics\",\"params\":{\"axis_formatter\":\"number\",\"axis_position\":\"left\",\"axis_scale\":\"normal\",\"background_color_rules\":[{\"id\":\"05174ff0-a34b-11ea-aca0-a5b495fa2259\"}],\"bar_color_rules\":[{\"id\":\"07e67580-a34b-11ea-aca0-a5b495fa2259\"}],\"default_index_pattern\":\"ifaces-stats-*\",\"default_timefield\":\"timestamp\",\"gauge_color_rules\":[{\"id\":\"095a29c0-a34b-11ea-aca0-a5b495fa2259\"}],\"gauge_inner_width\":10,\"gauge_style\":\"half\",\"gauge_width\":10,\"id\":\"61ca57f0-469d-11e7-af02-69e470af7417\",\"index_pattern\":\"ifaces-stats-*\",\"interval\":\"auto\",\"isModelInvalid\":false,\"series\":[{\"axis_min\":\"0\",\"axis_position\":\"right\",\"chart_type\":\"line\",\"color\":\"rgba(188,0,0,1)\",\"fill\":0.5,\"filter\":{\"language\":\"kuery\",\"query\":\"\"},\"formatter\":\"number\",\"id\":\"61ca57f1-469d-11e7-af02-69e470af7417\",\"label\":\"rx_errors\",\"line_width\":1,\"metrics\":[{\"field\":\"rx_errors\",\"id\":\"61ca57f2-469d-11e7-af02-69e470af7417\",\"type\":\"avg\"},{\"field\":\"61ca57f2-469d-11e7-af02-69e470af7417\",\"id\":\"f52bd7a0-a34a-11ea-aca0-a5b495fa2259\",\"lag\":\"\",\"type\":\"serial_diff\"}],\"point_size\":1,\"separate_axis\":0,\"split_filters\":[{\"color\":\"#68BC00\",\"filter\":{\"language\":\"kuery\",\"query\":\"\"},\"id\":\"51dba4a0-b1fe-11ea-b7b0-cbda6a4b1cb5\"}],\"split_mode\":\"everything\",\"stacked\":\"none\",\"terms_field\":\"hostname\",\"terms_order_by\":\"_key\",\"terms_size\":\"1000\",\"type\":\"timeseries\"},{\"axis_position\":\"right\",\"chart_type\":\"line\",\"color\":\"rgba(242,173,0,1)\",\"fill\":0.5,\"formatter\":\"number\",\"id\":\"f4386e90-b1fe-11ea-b7b0-cbda6a4b1cb5\",\"label\":\"tx_errors\",\"line_width\":1,\"metrics\":[{\"field\":\"tx_errors\",\"id\":\"f4386e91-b1fe-11ea-b7b0-cbda6a4b1cb5\",\"type\":\"avg\"},{\"field\":\"f4386e91-b1fe-11ea-b7b0-cbda6a4b1cb5\",\"id\":\"a09a6030-b1ff-11ea-b7b0-cbda6a4b1cb5\",\"lag\":\"\",\"type\":\"serial_diff\"}],\"point_size\":1,\"separate_axis\":0,\"split_mode\":\"everything\",\"stacked\":\"none\",\"type\":\"timeseries\"}],\"show_grid\":1,\"show_legend\":1,\"time_field\":\"timestamp\",\"type\":\"timeseries\"},\"aggs\":[]}",
				"uiStateJSON": "{}",
				"description": "",
				"version": 1,
				"kibanaSavedObjectMeta": {
					"searchSourceJSON": "{}"
				}
			},
			"references": [],
			"migrationVersion": {
				"visualization": "7.4.2"
			}
		},
		{
			"id": "58b61450-b653-11ea-bd24-adcf27ac7621",
			"type": "visualization",
			"attributes": {
				"title": "multicast",
				"visState": "{\"title\":\"multicast\",\"type\":\"metrics\",\"params\":{\"axis_formatter\":\"number\",\"axis_position\":\"left\",\"axis_scale\":\"normal\",\"background_color_rules\":[{\"id\":\"05174ff0-a34b-11ea-aca0-a5b495fa2259\"}],\"bar_color_rules\":[{\"id\":\"07e67580-a34b-11ea-aca0-a5b495fa2259\"}],\"default_index_pattern\":\"ifaces-stats-*\",\"default_timefield\":\"timestamp\",\"gauge_color_rules\":[{\"id\":\"095a29c0-a34b-11ea-aca0-a5b495fa2259\"}],\"gauge_inner_width\":10,\"gauge_style\":\"half\",\"gauge_width\":10,\"id\":\"61ca57f0-469d-11e7-af02-69e470af7417\",\"index_pattern\":\"ifaces-stats-*\",\"interval\":\"auto\",\"isModelInvalid\":false,\"series\":[{\"axis_min\":\"0\",\"axis_position\":\"right\",\"chart_type\":\"line\",\"color\":\"#68BC00\",\"fill\":0.5,\"filter\":{\"language\":\"kuery\",\"query\":\"\"},\"formatter\":\"number\",\"id\":\"61ca57f1-469d-11e7-af02-69e470af7417\",\"label\":\"multicast\",\"line_width\":1,\"metrics\":[{\"field\":\"multicast\",\"id\":\"61ca57f2-469d-11e7-af02-69e470af7417\",\"type\":\"avg\"},{\"field\":\"61ca57f2-469d-11e7-af02-69e470af7417\",\"id\":\"f52bd7a0-a34a-11ea-aca0-a5b495fa2259\",\"lag\":\"\",\"type\":\"serial_diff\"}],\"point_size\":1,\"separate_axis\":0,\"split_filters\":[{\"color\":\"#68BC00\",\"filter\":{\"language\":\"kuery\",\"query\":\"\"},\"id\":\"51dba4a0-b1fe-11ea-b7b0-cbda6a4b1cb5\"}],\"split_mode\":\"everything\",\"stacked\":\"none\",\"terms_field\":\"hostname\",\"terms_order_by\":\"_key\",\"terms_size\":\"1000\",\"type\":\"timeseries\"}],\"show_grid\":1,\"show_legend\":1,\"time_field\":\"timestamp\",\"type\":\"timeseries\"},\"aggs\":[]}",
				"uiStateJSON": "{}",
				"description": "",
				"version": 1,
				"kibanaSavedObjectMeta": {
					"searchSourceJSON": "{}"
				}
			},
			"references": [],
			"migrationVersion": {
				"visualization": "7.4.2"
			}
		},
		{
			"id": "aa6a9040-b654-11ea-bd24-adcf27ac7621",
			"type": "visualization",
			"attributes": {
				"title": "errors_details",
				"visState": "{\"title\":\"errors_details\",\"type\":\"metrics\",\"params\":{\"axis_formatter\":\"number\",\"axis_position\":\"left\",\"axis_scale\":\"normal\",\"background_color_rules\":[{\"id\":\"05174ff0-a34b-11ea-aca0-a5b495fa2259\"}],\"bar_color_rules\":[{\"id\":\"07e67580-a34b-11ea-aca0-a5b495fa2259\"}],\"default_index_pattern\":\"ifaces-stats-*\",\"default_timefield\":\"timestamp\",\"gauge_color_rules\":[{\"id\":\"095a29c0-a34b-11ea-aca0-a5b495fa2259\"}],\"gauge_inner_width\":10,\"gauge_style\":\"half\",\"gauge_width\":10,\"id\":\"61ca57f0-469d-11e7-af02-69e470af7417\",\"index_pattern\":\"ifaces-stats-*\",\"interval\":\"auto\",\"isModelInvalid\":false,\"series\":[{\"axis_min\":\"0\",\"axis_position\":\"right\",\"chart_type\":\"line\",\"color\":\"rgba(250,40,255,1)\",\"fill\":0.5,\"filter\":{\"language\":\"kuery\",\"query\":\"\"},\"formatter\":\"number\",\"id\":\"61ca57f1-469d-11e7-af02-69e470af7417\",\"label\":\"rx_dropped\",\"line_width\":1,\"metrics\":[{\"field\":\"rx_dropped\",\"id\":\"61ca57f2-469d-11e7-af02-69e470af7417\",\"type\":\"avg\"},{\"field\":\"61ca57f2-469d-11e7-af02-69e470af7417\",\"id\":\"f52bd7a0-a34a-11ea-aca0-a5b495fa2259\",\"lag\":\"\",\"type\":\"serial_diff\"}],\"point_size\":1,\"separate_axis\":0,\"split_filters\":[{\"color\":\"#68BC00\",\"filter\":{\"language\":\"kuery\",\"query\":\"\"},\"id\":\"51dba4a0-b1fe-11ea-b7b0-cbda6a4b1cb5\"}],\"split_mode\":\"everything\",\"stacked\":\"none\",\"terms_field\":\"hostname\",\"terms_order_by\":\"_key\",\"terms_size\":\"1000\",\"type\":\"timeseries\"},{\"axis_position\":\"right\",\"chart_type\":\"line\",\"color\":\"rgba(242,173,0,1)\",\"fill\":0.5,\"formatter\":\"number\",\"id\":\"f4386e90-b1fe-11ea-b7b0-cbda6a4b1cb5\",\"label\":\"tx_dropped\",\"line_width\":1,\"metrics\":[{\"field\":\"tx_dropped\",\"id\":\"f4386e91-b1fe-11ea-b7b0-cbda6a4b1cb5\",\"type\":\"avg\"},{\"field\":\"f4386e91-b1fe-11ea-b7b0-cbda6a4b1cb5\",\"id\":\"a09a6030-b1ff-11ea-b7b0-cbda6a4b1cb5\",\"lag\":\"\",\"type\":\"serial_diff\"}],\"point_size\":1,\"separate_axis\":0,\"split_mode\":\"everything\",\"stacked\":\"none\",\"type\":\"timeseries\"},{\"axis_position\":\"right\",\"chart_type\":\"line\",\"color\":\"rgba(149,242,0,1)\",\"fill\":0.5,\"formatter\":\"number\",\"id\":\"8a2f06e0-b653-11ea-ab15-391490d9a269\",\"label\":\"collisions\",\"line_width\":1,\"metrics\":[{\"field\":\"collisions\",\"id\":\"8a2f06e1-b653-11ea-ab15-391490d9a269\",\"type\":\"avg\"},{\"field\":\"8a2f06e1-b653-11ea-ab15-391490d9a269\",\"id\":\"8a2f06e2-b653-11ea-ab15-391490d9a269\",\"lag\":\"\",\"type\":\"serial_diff\"}],\"point_size\":1,\"separate_axis\":0,\"split_mode\":\"everything\",\"stacked\":\"none\",\"type\":\"timeseries\"},{\"axis_position\":\"right\",\"chart_type\":\"line\",\"color\":\"rgba(128,137,0,1)\",\"fill\":0.5,\"formatter\":\"number\",\"id\":\"a2c45ca0-b653-11ea-ab15-391490d9a269\",\"label\":\"rx_length_errors\",\"line_width\":1,\"metrics\":[{\"field\":\"rx_length_errors\",\"id\":\"a2c45ca1-b653-11ea-ab15-391490d9a269\",\"type\":\"avg\"},{\"field\":\"a2c45ca1-b653-11ea-ab15-391490d9a269\",\"id\":\"a2c45ca2-b653-11ea-ab15-391490d9a269\",\"lag\":\"\",\"type\":\"serial_diff\"}],\"point_size\":1,\"separate_axis\":0,\"split_mode\":\"everything\",\"stacked\":\"none\",\"type\":\"timeseries\"},{\"axis_position\":\"right\",\"chart_type\":\"line\",\"color\":\"rgba(128,137,0,1)\",\"fill\":0.5,\"formatter\":\"number\",\"id\":\"bd5ad990-b653-11ea-ab15-391490d9a269\",\"label\":\"rx_over_errors\",\"line_width\":1,\"metrics\":[{\"field\":\"rx_over_errors\",\"id\":\"bd5ad991-b653-11ea-ab15-391490d9a269\",\"type\":\"avg\"},{\"field\":\"bd5ad991-b653-11ea-ab15-391490d9a269\",\"id\":\"bd5ad992-b653-11ea-ab15-391490d9a269\",\"lag\":\"\",\"type\":\"serial_diff\"}],\"point_size\":1,\"separate_axis\":0,\"split_mode\":\"everything\",\"stacked\":\"none\",\"type\":\"timeseries\"},{\"axis_position\":\"right\",\"chart_type\":\"line\",\"color\":\"rgba(251,158,0,1)\",\"fill\":0.5,\"formatter\":\"number\",\"id\":\"d4490f00-b653-11ea-ab15-391490d9a269\",\"label\":\"rx_crc_errors\",\"line_width\":1,\"metrics\":[{\"field\":\"rx_crc_errors\",\"id\":\"d4490f01-b653-11ea-ab15-391490d9a269\",\"type\":\"avg\"},{\"field\":\"d4490f01-b653-11ea-ab15-391490d9a269\",\"id\":\"d4490f02-b653-11ea-ab15-391490d9a269\",\"lag\":\"\",\"type\":\"serial_diff\"}],\"point_size\":1,\"separate_axis\":0,\"split_mode\":\"everything\",\"stacked\":\"none\",\"type\":\"timeseries\"},{\"axis_position\":\"right\",\"chart_type\":\"line\",\"color\":\"rgba(123,100,255,1)\",\"fill\":0.5,\"formatter\":\"number\",\"id\":\"ee33e2a0-b653-11ea-ab15-391490d9a269\",\"label\":\"rx_frame_errors\",\"line_width\":1,\"metrics\":[{\"field\":\"rx_frame_errors\",\"id\":\"ee33e2a1-b653-11ea-ab15-391490d9a269\",\"type\":\"avg\"},{\"field\":\"ee33e2a1-b653-11ea-ab15-391490d9a269\",\"id\":\"ee33e2a2-b653-11ea-ab15-391490d9a269\",\"lag\":\"\",\"type\":\"serial_diff\"}],\"point_size\":1,\"separate_axis\":0,\"split_mode\":\"everything\",\"stacked\":\"none\",\"type\":\"timeseries\"},{\"axis_position\":\"right\",\"chart_type\":\"line\",\"color\":\"rgba(171,20,158,1)\",\"fill\":0.5,\"formatter\":\"number\",\"id\":\"0f9d0c00-b654-11ea-ab15-391490d9a269\",\"label\":\"rx_fifo_errors\",\"line_width\":1,\"metrics\":[{\"field\":\"rx_fifo_errors\",\"id\":\"0f9d0c01-b654-11ea-ab15-391490d9a269\",\"type\":\"avg\"},{\"field\":\"0f9d0c01-b654-11ea-ab15-391490d9a269\",\"id\":\"0f9d0c02-b654-11ea-ab15-391490d9a269\",\"lag\":\"\",\"type\":\"serial_diff\"}],\"point_size\":1,\"separate_axis\":0,\"split_mode\":\"everything\",\"stacked\":\"none\",\"type\":\"timeseries\"},{\"axis_position\":\"right\",\"chart_type\":\"line\",\"color\":\"rgba(101,50,148,1)\",\"fill\":0.5,\"formatter\":\"number\",\"id\":\"2adfce80-b654-11ea-ab15-391490d9a269\",\"label\":\"rx_missed_errors\",\"line_width\":1,\"metrics\":[{\"field\":\"rx_missed_errors\",\"id\":\"2adfce81-b654-11ea-ab15-391490d9a269\",\"type\":\"avg\"},{\"field\":\"2adfce81-b654-11ea-ab15-391490d9a269\",\"id\":\"2adfce82-b654-11ea-ab15-391490d9a269\",\"lag\":\"\",\"type\":\"serial_diff\"}],\"point_size\":1,\"separate_axis\":0,\"split_mode\":\"everything\",\"stacked\":\"none\",\"type\":\"timeseries\"},{\"axis_position\":\"right\",\"chart_type\":\"line\",\"color\":\"rgba(101,50,148,1)\",\"fill\":0.5,\"formatter\":\"number\",\"id\":\"3e3cedf0-b654-11ea-ab15-391490d9a269\",\"label\":\"tx_aborted_errors\",\"line_width\":1,\"metrics\":[{\"field\":\"tx_aborted_errors\",\"id\":\"3e3cedf1-b654-11ea-ab15-391490d9a269\",\"type\":\"avg\"},{\"field\":\"3e3cedf1-b654-11ea-ab15-391490d9a269\",\"id\":\"3e3cedf2-b654-11ea-ab15-391490d9a269\",\"lag\":\"\",\"type\":\"serial_diff\"}],\"point_size\":1,\"separate_axis\":0,\"split_mode\":\"everything\",\"stacked\":\"none\",\"type\":\"timeseries\"},{\"axis_position\":\"right\",\"chart_type\":\"line\",\"color\":\"rgba(123,100,255,1)\",\"fill\":0.5,\"formatter\":\"number\",\"id\":\"532c1f10-b654-11ea-ab15-391490d9a269\",\"label\":\"tx_carrier_errors\",\"line_width\":1,\"metrics\":[{\"field\":\"tx_carrier_errors\",\"id\":\"532c1f11-b654-11ea-ab15-391490d9a269\",\"type\":\"avg\"},{\"field\":\"532c1f11-b654-11ea-ab15-391490d9a269\",\"id\":\"532c1f12-b654-11ea-ab15-391490d9a269\",\"lag\":\"\",\"type\":\"serial_diff\"}],\"point_size\":1,\"separate_axis\":0,\"split_mode\":\"everything\",\"stacked\":\"none\",\"type\":\"timeseries\"},{\"axis_position\":\"right\",\"chart_type\":\"line\",\"color\":\"rgba(219,223,0,1)\",\"fill\":0.5,\"formatter\":\"number\",\"id\":\"693c0270-b654-11ea-ab15-391490d9a269\",\"label\":\"tx_fifo_errors\",\"line_width\":1,\"metrics\":[{\"field\":\"tx_fifo_errors\",\"id\":\"693c0271-b654-11ea-ab15-391490d9a269\",\"type\":\"avg\"},{\"field\":\"693c0271-b654-11ea-ab15-391490d9a269\",\"id\":\"693c0272-b654-11ea-ab15-391490d9a269\",\"lag\":\"\",\"type\":\"serial_diff\"}],\"point_size\":1,\"separate_axis\":0,\"split_mode\":\"everything\",\"stacked\":\"none\",\"type\":\"timeseries\"},{\"axis_position\":\"right\",\"chart_type\":\"line\",\"color\":\"rgba(164,221,0,1)\",\"fill\":0.5,\"formatter\":\"number\",\"id\":\"814803f0-b654-11ea-ab15-391490d9a269\",\"label\":\"tx_heartbeat_errors\",\"line_width\":1,\"metrics\":[{\"field\":\"tx_heartbeat_errors\",\"id\":\"814803f1-b654-11ea-ab15-391490d9a269\",\"type\":\"avg\"},{\"field\":\"814803f1-b654-11ea-ab15-391490d9a269\",\"id\":\"814803f2-b654-11ea-ab15-391490d9a269\",\"lag\":\"\",\"type\":\"serial_diff\"}],\"point_size\":1,\"separate_axis\":0,\"split_mode\":\"everything\",\"stacked\":\"none\",\"type\":\"timeseries\"}],\"show_grid\":1,\"show_legend\":1,\"time_field\":\"timestamp\",\"type\":\"timeseries\"},\"aggs\":[]}",
				"uiStateJSON": "{}",
				"description": "",
				"version": 1,
				"kibanaSavedObjectMeta": {
					"searchSourceJSON": "{}"
				}
			},
			"references": [],
			"migrationVersion": {
				"visualization": "7.4.2"
			}
		},
		{
			"id": "3f410260-b652-11ea-bd24-adcf27ac7621",
			"type": "visualization",
			"attributes": {
				"title": "packets",
				"visState": "{\"title\":\"packets\",\"type\":\"metrics\",\"params\":{\"axis_formatter\":\"number\",\"axis_position\":\"left\",\"axis_scale\":\"normal\",\"background_color_rules\":[{\"id\":\"05174ff0-a34b-11ea-aca0-a5b495fa2259\"}],\"bar_color_rules\":[{\"id\":\"07e67580-a34b-11ea-aca0-a5b495fa2259\"}],\"default_index_pattern\":\"ifaces-stats-*\",\"default_timefield\":\"timestamp\",\"gauge_color_rules\":[{\"id\":\"095a29c0-a34b-11ea-aca0-a5b495fa2259\"}],\"gauge_inner_width\":10,\"gauge_style\":\"half\",\"gauge_width\":10,\"id\":\"61ca57f0-469d-11e7-af02-69e470af7417\",\"index_pattern\":\"ifaces-stats-*\",\"interval\":\"auto\",\"isModelInvalid\":false,\"series\":[{\"axis_min\":\"0\",\"axis_position\":\"right\",\"chart_type\":\"line\",\"color\":\"#68BC00\",\"fill\":0.5,\"filter\":{\"language\":\"kuery\",\"query\":\"\"},\"formatter\":\"number\",\"id\":\"61ca57f1-469d-11e7-af02-69e470af7417\",\"label\":\"rx(packets)\",\"line_width\":1,\"metrics\":[{\"field\":\"rx_packets\",\"id\":\"61ca57f2-469d-11e7-af02-69e470af7417\",\"type\":\"avg\"},{\"field\":\"61ca57f2-469d-11e7-af02-69e470af7417\",\"id\":\"f52bd7a0-a34a-11ea-aca0-a5b495fa2259\",\"lag\":\"\",\"type\":\"serial_diff\"}],\"point_size\":1,\"separate_axis\":0,\"split_filters\":[{\"color\":\"#68BC00\",\"filter\":{\"language\":\"kuery\",\"query\":\"\"},\"id\":\"51dba4a0-b1fe-11ea-b7b0-cbda6a4b1cb5\"}],\"split_mode\":\"everything\",\"stacked\":\"none\",\"terms_field\":\"hostname\",\"terms_order_by\":\"_key\",\"terms_size\":\"1000\",\"type\":\"timeseries\"},{\"axis_position\":\"right\",\"chart_type\":\"line\",\"color\":\"rgba(188,0,121,1)\",\"fill\":0.5,\"formatter\":\"number\",\"id\":\"f4386e90-b1fe-11ea-b7b0-cbda6a4b1cb5\",\"label\":\"tx(packets)\",\"line_width\":1,\"metrics\":[{\"field\":\"tx_packets\",\"id\":\"f4386e91-b1fe-11ea-b7b0-cbda6a4b1cb5\",\"type\":\"avg\"},{\"field\":\"f4386e91-b1fe-11ea-b7b0-cbda6a4b1cb5\",\"id\":\"a09a6030-b1ff-11ea-b7b0-cbda6a4b1cb5\",\"lag\":\"\",\"type\":\"serial_diff\"}],\"point_size\":1,\"separate_axis\":0,\"split_mode\":\"everything\",\"stacked\":\"none\",\"type\":\"timeseries\"}],\"show_grid\":1,\"show_legend\":1,\"time_field\":\"timestamp\",\"type\":\"timeseries\"},\"aggs\":[]}",
				"uiStateJSON": "{}",
				"description": "",
				"version": 1,
				"kibanaSavedObjectMeta": {
					"searchSourceJSON": "{}"
				}
			},
			"references": [],
			"migrationVersion": {
				"visualization": "7.4.2"
			}
		},
		{
			"id": "fce69ec0-b651-11ea-bd24-adcf27ac7621",
			"type": "visualization",
			"attributes": {
				"title": "bytes",
				"visState": "{\"title\":\"bytes\",\"type\":\"metrics\",\"params\":{\"axis_formatter\":\"number\",\"axis_position\":\"left\",\"axis_scale\":\"normal\",\"background_color_rules\":[{\"id\":\"05174ff0-a34b-11ea-aca0-a5b495fa2259\"}],\"bar_color_rules\":[{\"id\":\"07e67580-a34b-11ea-aca0-a5b495fa2259\"}],\"default_index_pattern\":\"ifaces-stats-*\",\"default_timefield\":\"timestamp\",\"gauge_color_rules\":[{\"id\":\"095a29c0-a34b-11ea-aca0-a5b495fa2259\"}],\"gauge_inner_width\":10,\"gauge_style\":\"half\",\"gauge_width\":10,\"id\":\"61ca57f0-469d-11e7-af02-69e470af7417\",\"index_pattern\":\"ifaces-stats-*\",\"interval\":\"auto\",\"isModelInvalid\":false,\"series\":[{\"axis_min\":\"0\",\"axis_position\":\"right\",\"chart_type\":\"line\",\"color\":\"#68BC00\",\"fill\":0.5,\"filter\":{\"language\":\"kuery\",\"query\":\"\"},\"formatter\":\"bytes\",\"id\":\"61ca57f1-469d-11e7-af02-69e470af7417\",\"label\":\"rx(bytes)\",\"line_width\":1,\"metrics\":[{\"field\":\"rx_bytes\",\"id\":\"61ca57f2-469d-11e7-af02-69e470af7417\",\"type\":\"avg\"},{\"field\":\"61ca57f2-469d-11e7-af02-69e470af7417\",\"id\":\"f52bd7a0-a34a-11ea-aca0-a5b495fa2259\",\"lag\":\"\",\"type\":\"serial_diff\"}],\"point_size\":1,\"separate_axis\":0,\"split_filters\":[{\"color\":\"#68BC00\",\"filter\":{\"language\":\"kuery\",\"query\":\"\"},\"id\":\"51dba4a0-b1fe-11ea-b7b0-cbda6a4b1cb5\"}],\"split_mode\":\"everything\",\"stacked\":\"none\",\"terms_field\":\"hostname\",\"terms_order_by\":\"_key\",\"terms_size\":\"1000\",\"type\":\"timeseries\"},{\"axis_position\":\"right\",\"chart_type\":\"line\",\"color\":\"rgba(188,0,121,1)\",\"fill\":0.5,\"formatter\":\"bytes\",\"id\":\"f4386e90-b1fe-11ea-b7b0-cbda6a4b1cb5\",\"label\":\"tx(bytes)\",\"line_width\":1,\"metrics\":[{\"field\":\"tx_bytes\",\"id\":\"f4386e91-b1fe-11ea-b7b0-cbda6a4b1cb5\",\"type\":\"avg\"},{\"field\":\"f4386e91-b1fe-11ea-b7b0-cbda6a4b1cb5\",\"id\":\"a09a6030-b1ff-11ea-b7b0-cbda6a4b1cb5\",\"lag\":\"\",\"type\":\"serial_diff\"}],\"point_size\":1,\"separate_axis\":0,\"split_mode\":\"everything\",\"stacked\":\"none\",\"type\":\"timeseries\"}],\"show_grid\":1,\"show_legend\":1,\"time_field\":\"timestamp\",\"type\":\"timeseries\"},\"aggs\":[]}",
				"uiStateJSON": "{}",
				"description": "",
				"version": 1,
				"kibanaSavedObjectMeta": {
					"searchSourceJSON": "{}"
				}
			},
			"references": [],
			"migrationVersion": {
				"visualization": "7.4.2"
			}
		}
		]
		}
	`)
	if err != nil {
		panic(err)
	}

	var output bytes.Buffer
	err = dashTmpl.Execute(&output, dash)
	if err != nil {
		panic(err)
	}
	//println(output.String())
	sendRequest(url+"api/kibana/dashboards/import", output.Bytes(), "POST")

}

func sendRequest(url string, data []byte, method string) {
	client := &http.Client{}
	req, err := http.NewRequest(method, url, bytes.NewBuffer(data))
	if err != nil {
		fmt.Println(err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	if strings.Contains(url, "kibana") {
		req.Header.Set("kbn-xsrf", "true")
	}
	response, err := client.Do(req)
	if err != nil {
		fmt.Println()
		fmt.Println(err)
		fmt.Printf("Error while trying to send req to Elastic. Can you join the API ? Passing instead of panic'ing...")
		return
	}
	if response.StatusCode > 299 {
		println(url + "  ----> Oops : " + response.Status)
	}

	_, err = ioutil.ReadAll(response.Body)
	if err != nil {
		fmt.Println(err)
	}
	response.Body.Close()
}
