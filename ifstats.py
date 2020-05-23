#! /usr/bin/env python3

from subprocess import run, PIPE
from json import loads, dumps
from time import time, sleep, strftime
from threading import Thread
import requests

ELASTIC_URL = f"http://localhost:9200/ifaces-stats-{strftime('%Y-%m-%d')}"
BULK_URL = f"http://localhost:9200/ifaces-stats-{strftime('%Y-%m-%d')}/_bulk"
MAPPING_URL = f"http://localhost:9200/ifaces-stats-{strftime('%Y-%m-%d')}/_mapping"
HEADERS = {'Content-Type': 'application/json'}
INDEX_SETTINGS = {
      "settings" : {
        "number_of_shards" : 2,
        "number_of_replicas" : 2
      }
}
INDEX_MAPPING = {
    "properties":{
    "timestamp": {"type": "date", "format": "epoch_millis"},
    "iface": { "type": "keyword" },
    "rx_packets": { "type": "long"},
    "tx_packets": { "type": "long"},
    "rx_bytes": { "type": "long"},
    "tx_bytes": { "type": "long"},
    "rx_errors": { "type": "long"},
    "tx_errors": { "type": "long"},
    "rx_dropped": { "type": "long"},
    "tx_dropped": { "type": "long"},
    "multicast": { "type": "long"},
    "collisions": { "type": "long"},
    "rx_length_errors": { "type": "long"},
    "rx_over_errors": { "type": "long"},
    "rx_crc_errors": { "type": "long"},
    "rx_frame_errors": { "type": "long"},
    "rx_fifo_errors": { "type": "long"},
    "rx_missed_errors": { "type": "long"},
    "tx_aborted_errors": { "type": "long"},
    "tx_carrier_errors": { "type": "long"},
    "tx_fifo_errors": { "type": "long"},
    "tx_heartbeat_errors": { "type": "long"}
    }
}

def get_stats_iface(iface):
    output = run(f'./ifstat-nxos.bin -a -s -e -z -j -p {iface}', shell=True, check=True, stdout=PIPE, universal_newlines=True).stdout
    return loads(output)

def get_stats_all_ifaces(ifaces):
    for iface in ifaces:
        yield get_stats_iface(iface)['kernel'][iface]
        

def loading_ifaces():
    ifaces = run(f"ip address show up | grep mtu | cut -f2 -d ':' | cut -f1 -d '@' | tr -d '\n'", shell=True, check=True, stdout=PIPE, universal_newlines=True).stdout
    return ifaces.split()

def convert_stats_to_bulk(stats):
    """ Must convert to 
{"index":{}}
{"Amount": "480", "Quantity": "2", "Id": "975463711", "Client_Store_sk": "1109"}
{"index":{}}
{"Amount": "2105", "Quantity": "2", "Id": "975463943", "Client_Store_sk": "1109"}
{"index":{}}
{"Amount": "2107", "Quantity": "3", "Id": "974920111", "Client_Store_sk": "1109"}
    """

    bulk_line_index_dict = { 'index': {} } 
    bulk_line_value_dict = {}
    stats_to_send = ''

    for timing, ifaces_data in stats.items():
        milli_timing = int(timing * 1000)

        for iface, stats in ifaces_data.items():
            bulk_line_value_dict['timestamp'] = milli_timing
            bulk_line_value_dict['iface'] = iface
            # Insert all the values of the interface
            for key, value in stats.items():
                bulk_line_value_dict[key] = value
            #stats_to_send += dumps(bulk_line_index_dict) + "\n"
            #stats_to_send += dumps(bulk_line_value_dict) + "\n"
            stats_to_send += dumps(bulk_line_index_dict) + '\n'
            stats_to_send += dumps(bulk_line_value_dict) + '\n'



    return stats_to_send

def ensure_index_and_mapping():
    """
curl -X PUT "localhost:9200/twitter?pretty" -H 'Content-Type: application/json' -d'
{
  "settings" : {
    "number_of_shards" : 1,
    "number_of_replicas" : 0
  }
}'
curl -XPUT "http://localhost:9200/twitter/_mapping" -H 'Content-Type: application/json' -d'
{
 "properties": {
  "post_time": { "type": "date" },
  "username":  { "type": "keyword" },
  "message":   { "type": "text" }
 }
}'
    """
    output = requests.put(url=ELASTIC_URL, headers=HEADERS, data=dumps(INDEX_SETTINGS))
    #print(dumps(output.json(),indent=2))
    output = requests.put(url=MAPPING_URL, headers=HEADERS, data=dumps(INDEX_MAPPING))
    #print(dumps(output.json(),indent=2))

def send_elastic(stats):
    """
    """
    ensure_index_and_mapping()

    stats_to_send = convert_stats_to_bulk(stats)

    #data_to_send = dumps(stats_to_send) + "\n" #New line needed at the end of a bulk req
    #print(stats_to_send)
    output = requests.post(url=BULK_URL, headers=HEADERS, data=stats_to_send)
    #print(dumps(output.json(),indent=2))

def ifaces_polling(ifaces):

    stats_to_send = {}

    previous_time = time()
    while True:
        sleep(.1)
        current_time = time()
        stats_to_send[current_time] = dict(zip(ifaces, get_stats_all_ifaces(ifaces)))
        if current_time - previous_time > 5:
            thread = Thread(target=send_elastic, args=(stats_to_send,))
            thread.start()
            stats_to_send = {}
            previous_time = current_time
        #print("--- %s seconds ---" % (time() - current_time))

def main():
    ifaces_polling(loading_ifaces())

if __name__ == "__main__":
    main()
