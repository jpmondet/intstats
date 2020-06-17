
import os
import flag
import time
import net.http
import json


struct Intstats {
  rx_packets          f64
  tx_packets          f64
  rx_bytes            f64
  tx_bytes            f64
  rx_errors           f64
  tx_errors           f64
  rx_dropped          f64
  tx_dropped          f64
  multicast           f64
  collisions          f64
  rx_length_errors    f64
  rx_over_errors      f64
  rx_crc_errors       f64
  rx_frame_errors     f64
  rx_fifo_errors      f64
  rx_missed_errors    f64
  tx_aborted_errors   f64
  tx_carrier_errors   f64
  tx_fifo_errors      f64
  tx_heartbeat_errors f64
}

struct Ifacestats {
  //ifacestats map[string]Intstats
  iface string [raw; json:string]
}

struct Ifstat_return {
  kernel string [raw; json:'kernel']
  //kernel ?map[string]string
  //iface Intstats
}

struct Bulk_line_intstats {
  mut:
    timestamp           i64
    hostname            string
    iface               string
    rx_packets          f64 [json:"rx_packets"]
    tx_packets          f64 [json:"tx_packets"]
    rx_bytes            f64 [json:"rx_bytes"]
    tx_bytes            f64 [json:"tx_bytes"]
    rx_errors           f64 [json:"rx_errors"]
    tx_errors           f64 [json:"tx_errors"]
    rx_dropped          f64 [json:"rx_dropped"]
    tx_dropped          f64 [json:"tx_dropped"]
    multicast           f64 [json:"multicast"]
    collisions          f64 [json:"collisions"]
    rx_length_errors    f64 [json:"rx_length_errors"]
    rx_over_errors      f64 [json:"rx_over_errors"]
    rx_crc_errors       f64 [json:"rx_crc_errors"]
    rx_frame_errors     f64 [json:"rx_frame_errors"]
    rx_fifo_errors      f64 [json:"rx_fifo_errors"]
    rx_missed_errors    f64 [json:"rx_missed_errors"]
    tx_aborted_errors   f64 [json:"tx_aborted_errors"]
    tx_carrier_errors   f64 [json:"tx_carrier_errors"]
    tx_fifo_errors      f64 [json:"tx_fifo_errors"]
    tx_heartbeat_errors f64 [json:"tx_heartbeat_errors"]
}



fn send_request(url string, data string, method string) {

  mut resp := http.Response{}

  if method == "PUT" {
    resp = http.put(url, json.encode(data)) or {
      println("PUT req failed to $url")
      return
    }
  } else if method == "POST" {
    resp = http.post(url, json.encode(data)) or {
      println("PUT req failed to $url")
      return
    }
  } else {
    resp = http.get(url) or {
      println("PUT req failed to $url")
      return
    }
  }
  println(resp.text)

}

fn  ensure_index_and_mapping(url string) {
  //mut index_settings := []byte{}
  index_settings := '{"settings": {"number_of_shards": 2, "number_of_replicas": 2}}'
  //mut index_mapping := []byte{}
  index_mapping := '
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
  }'

  send_request(url, index_settings, "PUT")
  send_request(url + "/_mapping", index_mapping, "PUT")

}

fn send_to_elastic(batch_to_send []Bulk_line_intstats, url string){
  json_index_str := '{"index":{}}\n'

  mut bulk_req := ""
  for bli in batch_to_send {
    bulk_req = bulk_req + json_index_str + json.encode(bli) + '\n'
  }
  println(bulk_req)
  send_request(url, bulk_req, "POST")

  /*
  ids := json.decode([]int{}, resp.body)? 
  mut cursor := 0
  for _ in 0..8 {
      go fn() {
          for {
              lock { 
                  if cursor >= ids.len {
                      break
                  }
                  id := ids[cursor]
                  cursor++
              }
              resp := http.get('$ItemUrlBase/$id.json')? 
              story := json.decode(Story, resp.body)?
              println(story.title)
          }
      }()
  }
  runtime.wait() 
  */

}

fn ifaces_monitoring(ifaces_list []string, binary string, netns string, retrieval_interval int, send_interval int, url string, hostname string) {

  ifstat_options := " -a -s -e -z -j -p "
  mut ifstat_cmd := binary + ifstat_options
  if netns != "" {
    ifstat_cmd = "ip netns exec " + netns + " " + ifstat_cmd
  }

  // Calculate time at which we must send to elastic
  //time_to_send := time.now().add_seconds(send_interval)

  // Pre-size the slice that will hold the ifaces datas
  //lenSlice := len(ifacesList) * (sendInterval * 1000 / retrievalInterval)
  ////batchSlice  := make([]map[string]interface{}, lenSlice)
  //batchSlice  := make([]bulkLineIntStats, lenSlice)
  //indexSlice := 0

  mut stopwatch := time.new_stopwatch(time.StopWatchOptions{true})
  stopwatch.start()
  mut bli := Bulk_line_intstats{}
  mut timestamp := time.now().unix_time()
  mut batch := []Bulk_line_intstats{}
  // Loop forever
  for {
    for iface in ifaces_list {
      ifstat_cmd_iface := ifstat_cmd + iface
      ifstat_ret := os.exec(ifstat_cmd_iface) or {
        println("Hmm, wasn't able to get iface stats...")
        continue
      }
      ifstat_json_str := ifstat_ret.output
      mut ifstat_struct := json.decode(Ifstat_return, ifstat_json_str) or {
		    eprintln('Failed to parse json')
		    return
	    }
      // vlan JSON lib being very alpha-ish we must process string manually :\
      ifstats_str := "{" + ifstat_struct.kernel.split(':{')[1].split('}')[0] + "}"

      bli = json.decode(Bulk_line_intstats, ifstats_str) or {
		    eprintln('Failed to parse json')
		    return
	    }
      bli.hostname = hostname
      bli.iface = iface
      bli.timestamp = timestamp + stopwatch.elapsed().milliseconds()
      batch.push(bli)
    }

    if stopwatch.elapsed().seconds() > send_interval {
      //go send_request()
      go send_to_elastic(batch, url)
      stopwatch.restart()
      timestamp = time.now().unix_time()
      batch = []Bulk_line_intstats{}
      //println("Restart stopwatch")
    }
    //timeInterval = timeInterval + retrievalInterval
    //if timeInterval > (sendInterval * 1000) {
    //if (timeInterval - timestamp) < 0 {
    //  //go formatAndsendToElastic(ifacesStats)
    //  go formatAndsendToElastic(batchSlice, url)
    //  timeInterval_t := time.Now()
    //  timeInterval_t = timeInterval_t.Add(time.Duration(sendInterval) * time.Second)
    //  timeInterval = timeInterval_t.UnixNano() / 1000000
    //  //batchSlice = make([]map[string]interface{}, lenSlice)
    //  batchSlice  = make([]bulkLineIntStats, lenSlice)
    //  indexSlice = 0
    //}
    //tNowAfter := time.Now() // for debug timing
    //fmt.Println(tNowAfter.Sub(tNowBefore))  // for debug timing
    time.sleep_ms(retrieval_interval)
    //println(time.now())
  }

}

fn main() {

  // Handling args
  mut flag_parser := flag.new_flag_parser(os.args)
 	flag_parser.application("intstatspy")
	flag_parser.version("v0.01")
	flag_parser.description("This binary is designed to collect interfaces stats and send them to elastic")
	//flag_parser.arguments_description("hello")
	flag_parser.skip_executable()
	show_help := flag_parser.bool('help', 0, false, 'Show this help screen\n')
  mut url := flag_parser.string("url", `u`, "http://127.0.0.1:9200/", "Url of Elastic cluster api")
  mut elastic_index := flag_parser.string("elasticIndex", `x`, "ifaces-stats-", "Index on which we must send in Elastic. Default : 'ifaces-stats-' and will add date automatically")
  netns := flag_parser.string("netns", `n`, "", "Namespace on which the interfaces should be retrieved")
  ifaces := flag_parser.string("ifaces", `i`, "", "List of interfaces that must be monitored (for example : 'eth0,eth1')\n By default, all the interfaces of the namespace are monitored. \n")
	binary := flag_parser.string("binary", `b`, "ifstat", "For now, 'ifstat' binary is used to retrieve interface stats. \nYou can specify the path of the binary if you don't want to use the \ndefault binary of your system.")
	send_interval := flag_parser.int("sendInterval", `s`, 300, "Interval at which bulk requests should be sent to elastic \nDefault : 300 seconds")
	retrieval_interval := flag_parser.int("retrievalInterval", `r`, 200, "Interval at which stats should be retrieved from interfaces \nDefault : 200 milliseconds")
	mut hostname := flag_parser.string("hostname", `h`, "", "Hostname of this sender. Will try to discover it by default")
  //additional_args := flag_parser.finalize() or {
  //    eprintln(err)
  //    println(flag_parser.usage())
  //    return
  //}
  //println(additional_args.join_lines())
  if show_help {
		println(flag_parser.usage())
		exit(0)
	}


  // Formatting & completing passed args
  elastic_index = elastic_index + time.now().ymmdd()
  if hostname == "" {
    hostname_res := os.exec('hostname') or {
      println("Hmm, wasn't able to get hostname...")
      return
    }
    //hostname = os.hostname()
    hostname = hostname_res.output.trim_space()
  }
  mut ifaces_list := []string{}
  if ifaces == "" {
    ls_ret := os.exec("ip address show up | grep mtu | cut -f2 -d ':' | cut -f1 -d '@' | tr -d '\n'") or {
      println("Hmm, wasn't able to get current interfaces in use...")
      return
    }
    ifaces_list = ls_ret.output.trim_space().split(' ')
  } else {
    ifaces_list = ifaces.trim_space().split(',')
  }

  println("Args that will be used : \n\t$url, $elastic_index, $netns, $ifaces_list, $binary, $send_interval, $retrieval_interval, $hostname")

  url = url + elastic_index

  ensure_index_and_mapping(url)

  url = url + "/_bulk"

  ifaces_monitoring(ifaces_list, binary, netns, retrieval_interval, send_interval, url, hostname)

}