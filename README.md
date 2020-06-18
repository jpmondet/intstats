# Intstats (or Ifstatspy, or even Intstatspy)

Retrieves host's interfaces statistics & send them to Elastic as a Bulk request.

Exemple of stats retrieved per iface :

```
"eth0": {
    "rx_packets": 142812,
    "tx_packets": 120873,
    "rx_bytes": 167791711,
    "tx_bytes": 19804263,
    "rx_errors": 0,
    "tx_errors": 0,
    "rx_dropped": 0,
    "tx_dropped": 0,
    "multicast": 0,
    "collisions": 0,
    "rx_length_errors": 0,
    "rx_over_errors": 0,
    "rx_crc_errors": 0,
    "rx_frame_errors": 0,
    "rx_fifo_errors": 0,
    "rx_missed_errors": 0,
    "tx_aborted_errors": 0,
    "tx_carrier_errors": 0,
    "tx_fifo_errors": 0,
    "tx_heartbeat_errors": 0
}
```

## Requirements
  - Linux 
  - `ifstat` binary (from [kernel.org's iproute2 miscs](https://git.kernel.org/pub/scm/network/iproute2/iproute2.git/tree/misc))
  - A working elastic cluster

## Exemple usage
