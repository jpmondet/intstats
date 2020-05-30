package main

import (
	"flag"
	"fmt"
	"time"
	"strings"
  "os/exec"
  "log"
  "encoding/json"
)

const (
  HOST      = "http://172.17.0.2"
	RECV_PORT = 9200
	TIMEOUT   = 5000
)

func main() {
	flag.Usage = func() {
		fmt.Printf("Usage:\n ifstatspy [options] \nOptions:\n")
		flag.PrintDefaults()
	}
  url := flag.String("u", "http://172.17.0.2:9200", "Url of Elastic cluster api")
  netns := flag.String("n", "", "Namespace on which the interfaces should be retrieved")
  ifaces := flag.String("i", "", "List of interfaces that must be monitored (for example : 'eth0,eth1')\n By default, all the interfaces of the namespace are monitored. \n")
	binary := flag.String("b", "ifstat", "For now, 'ifstat' binary is used to retrieve interface stats. \nYou can specify the path of the binary if you don't want to use the \ndefault binary of your system.")
	sendInterval := flag.Int("s", 300, "Interval at which bulk requests should be sent to elastic \nDefault : 300 seconds")
	retrievalInterval := flag.Int("r", 200, "Interval at which stats should be retrieved from interfaces \nDefault : 200 milliseconds")
	flag.Parse()

  fmt.Printf("Options used : %v, %v, %v, %v, %v, %v\n", *url, *netns, *ifaces, *binary, *sendInterval, *retrievalInterval)

  ifacesList := getIfaces(*ifaces)

  fmt.Printf("Ifaces to monitor : %q\n", ifacesList)

  ifacesMonitoring(ifacesList, *binary, *netns, *retrievalInterval, *sendInterval)
}

func getIfaces(ifaces string) []string{
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

func ifacesMonitoring(ifacesList []string, binary string, netns string, retrievalInterval int, sendInterval int) {
  var ifacesStats map[string]interface{}
  ifacesStats = make(map[string]interface{})

  options := " -a -s -e -z -j -p "
  var ifstat_cmd string
  if netns != "" {
    ifstat_cmd = "ip netns exec " + netns + " "+ binary + options
  } else {
    ifstat_cmd = binary + options
  }

  timeInterval := 0
  for {
    time.Sleep(time.Duration(retrievalInterval) * time.Millisecond)
    for _, iface := range ifacesList {
      ifstatCmd := ifstat_cmd + iface
      cmdOutput, err := exec.Command("bash", "-c", ifstatCmd).Output()
      if err != nil {
        log.Fatal(err)
      }
      var out map[string]interface{}
      if err := json.Unmarshal(cmdOutput, &out); err != nil {
        log.Fatal(err)
      }
      ifaceStats := out["kernel"].(map[string]interface{})
      ifacesStats[iface] = ifaceStats[iface].(map[string]interface{})
    }
    timeInterval = timeInterval + retrievalInterval
    if timeInterval > (sendInterval * 1000) {
      fmt.Println(ifacesStats)
      timeInterval = 0
    }
  }

}
