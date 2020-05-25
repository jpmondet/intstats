#! /usr/bin/env python3

from subprocess import run, PIPE
from json import loads, dumps
from time import time, sleep, strftime
from threading import Thread
from argparse import ArgumentParser
import requests

INDEX_NAME = f"ifaces-stats-{strftime('%Y-%m-%d')}-2"
ELASTIC_URL = f"/{INDEX_NAME}"
BULK_URL = f"{ELASTIC_URL}/_bulk"
MAPPING_URL = f"{ELASTIC_URL}/_mapping"
HEADERS = {"Content-Type": "application/json"}
INDEX_SETTINGS = {"settings": {"number_of_shards": 2, "number_of_replicas": 2}}
INDEX_MAPPING = {
    "properties": {
        "timestamp": {"type": "date", "format": "epoch_millis"},
        "iface": {"type": "keyword"},
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
        "tx_heartbeat_errors": {"type": "long"},
    }
}


def get_stats_iface(iface, netns, ifstat_binary):
    netns_cmd = f"ip netns exec {netns} " if netns else ""
    ifstat_bin = f"{ifstat_binary}" if ifstat_binary else "ifstat"
    output = run(
        f"{netns_cmd}{ifstat_bin} -a -s -e -z -j -p {iface}",
        shell=True,
        check=True,
        stdout=PIPE,
        universal_newlines=True,
    ).stdout
    return loads(output)


def get_stats_all_ifaces(ifaces, netns, ifstat_binary):
    for iface in ifaces:
        yield get_stats_iface(iface, netns, ifstat_binary)["kernel"][iface]


def loading_ifaces(ifaces_list, netns):
    if ifaces_list:
        return ifaces_list.split(',')

    netns_cmd = f"ip netns exec {netns} " if netns else ""
    ifaces = run(
        f"{netns_cmd}ip address show up | grep mtu | cut -f2 -d ':' | cut -f1 -d '@' | tr -d '\n'",
        shell=True,
        check=True,
        stdout=PIPE,
        universal_newlines=True,
    ).stdout
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

    bulk_line_index_dict = {"index": {}}
    bulk_line_value_dict = {}
    stats_to_send = ""

    for timing, ifaces_data in stats.items():
        milli_timing = int(timing * 1000)

        for iface, stats in ifaces_data.items():
            bulk_line_value_dict["timestamp"] = milli_timing
            bulk_line_value_dict["iface"] = iface
            # Insert all the values of the interface
            for key, value in stats.items():
                if "bytes" in key: 
                    kbits = key[:3] + "bits"
                    bulk_line_value_dict[kbits] = value * 8
                    continue
                bulk_line_value_dict[key] = value
            # stats_to_send += dumps(bulk_line_index_dict) + "\n"
            # stats_to_send += dumps(bulk_line_value_dict) + "\n"
            stats_to_send += dumps(bulk_line_index_dict) + "\n"
            stats_to_send += dumps(bulk_line_value_dict) + "\n"

    return stats_to_send


def ensure_index_and_mapping(elastic_base_url):
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
    output = requests.put(url=elastic_base_url+ELASTIC_URL, headers=HEADERS, data=dumps(INDEX_SETTINGS))
    #print(dumps(output.json(),indent=2))
    output = requests.put(url=elastic_base_url+MAPPING_URL, headers=HEADERS, data=dumps(INDEX_MAPPING))
    #print(dumps(output.json(),indent=2))


def send_elastic(stats, elastic_base_url):
    """
    """
    ensure_index_and_mapping(elastic_base_url)

    stats_to_send = convert_stats_to_bulk(stats)

    # data_to_send = dumps(stats_to_send) + "\n" #New line needed at the end of a bulk req
    # print(stats_to_send)
    output = requests.post(url=elastic_base_url+BULK_URL, headers=HEADERS, data=stats_to_send)
    #print(dumps(output.json(),indent=2))


def ifaces_polling(ifaces, netns, es_url, ifstat_binary):

    stats_to_send = {}

    previous_time = time()
    while True:
        sleep(0.2)
        current_time = time()
        stats_to_send[current_time] = dict(zip(ifaces, get_stats_all_ifaces(ifaces, netns, ifstat_binary)))
        if current_time - previous_time > 5:
            thread = Thread(target=send_elastic, args=(stats_to_send,es_url,))
            thread.start()
            stats_to_send = {}
            previous_time = current_time
        # print("--- %s seconds ---" % (time() - current_time))


def main():
    parser = ArgumentParser(
        description="Collects stats and send them to an elastic cluster"
    )
    parser.add_argument(
        "-u",
        "--url",
        type=str,
        help="URL of the elastic cluster api",
        default="http://localhost:9200",
    )
    parser.add_argument(
        "-n",
        "--netns",
        type=str,
        help="Namespace on which the interfaces should be retrieved",
    )
    parser.add_argument(
        "-i",
        "--ifaces",
        type=str,
        help="List of interfaces that must be monitored.\n \
             By default, all the interfaces of the namespace are monitored. \n"
    )
    parser.add_argument(
        "-b",
        "--binary",
        type=str,
        help="For now, 'ifstat' binary is used to retrieve interface stats. \n \
            You can specify the path of the binary if you don't want to use the \n \
            default binary of your system."
    )

    args = parser.parse_args()

    ifaces_polling(loading_ifaces(args.ifaces, args.netns),args.netns, args.url, args.binary)


if __name__ == "__main__":
    main()
