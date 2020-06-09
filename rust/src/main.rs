//#[macro_use]
extern crate json;

use clap::{Arg, App};
use std::process::{Command, Stdio};
use std::io::Error;

fn get_ifaces() -> Result<std::vec::Vec<str>,Error> {


    let mut ifaces_child = Command::new("ip")
            .arg("address")
            .arg("show")
            .arg("up")
            .stdout(Stdio::piped())
            .spawn()
            .expect("ip cmd failed");
    //let ifaces_stdout = ifaces_child.wait_with_output()?;
    //println!(
    //        "iface :{:#?}",String::from_utf8(ifaces_stdout.stdout).unwrap()
    //);

    if let Some(ifaces) = ifaces_child.stdout.take() {
        let mut mtu_child = Command::new("grep")
                .arg("mtu")
                .stdin(ifaces)
                .stdout(Stdio::piped())
                .spawn()
                .expect("grep failed");
        //let mtu_stdout = mtu_child.wait_with_output()?;
        //println!(
        //        "iface :{:#?}",String::from_utf8(mtu_stdout.stdout).unwrap()
        //);

        ifaces_child.wait()?;

        if let Some(mtu) = mtu_child.stdout.take() {
            let mut cut_child = Command::new("cut")
                    .arg("-f2")
                    .arg("-d")
                    .arg(":")
                    .stdin(mtu)
                    .stdout(Stdio::piped())
                    .spawn()
                    .expect("Cut failed");

            mtu_child.wait()?;

            if let Some(cut) = cut_child.stdout.take() {
                let mut cut2_child = Command::new("cut")
                        .arg("-f1")
                        .arg("-d")
                        .arg("@")
                        .stdin(cut)
                        .stdout(Stdio::piped())
                        .spawn()?;

                cut_child.wait()?;
                println!(
                    "iface :{:#?}",cut_child
                );

                if let Some(cut2) = cut2_child.stdout.take() {
                    let mut tr_child = Command::new("tr")
                            .arg("-d")
                            .arg("'\n'")
                            .stdin(cut2)
                            .stdout(Stdio::piped())
                            .spawn()?;

                    let tr_stdout = tr_child.wait_with_output()?;
                    
                    cut2_child.wait()?;
                    println!(
                            "iface :{:#?}",String::from_utf8(tr_stdout.stdout).unwrap()
                    );
                    let split = String::from_utf8(tr_stdout.stdout).unwrap().split(' ');
                    let mut vec_ifaces = Vec::new();
                    vec_ifaces = split.collect();
                    return Ok(vec_ifaces)
                }
            }
        }

    }

    return Err(Vec::new());
    
}

fn main() {
    let matches = App::new("Ifstatspy")
        .version("0.1.0")
        .author("https://github.com/jpmondet")
        .about("Retrieves statistics from network interfaces and send them as bulk to Elastic")
        .arg(Arg::with_name("url")
                 .short('u')
                 .long("url")
                 .takes_value(true)
                 .about("Url of Elastic cluster api"))
        .arg(Arg::with_name("elastic_index")
                 .short('x')
                 .long("index")
                 .takes_value(true)
                 .about("Index on which we must send in Elastic. Default : 'ifaces-stats-' and will add date automatically"))
        .arg(Arg::with_name("interfaces")
                 .short('i')
                 .long("interfaces")
                 .takes_value(true)
                 .about("Interfaces we must monitor"))
        .arg(Arg::with_name("binary")
                 .short('b')
                 .long("binary")
                 .takes_value(true)
                 .about("By default, 'ifstat' binary of your system is used. If you prefer to use another binary, indicate it with this option"))
        .arg(Arg::with_name("send_interval")
                 .short('s')
                 .long("send_interval")
                 .takes_value(true)
                 .about("Interval at which bulk requests should be sent to elastic \nDefault : 300 seconds"))
        .arg(Arg::with_name("retrieve_interval")
                 .short('r')
                 .long("retrieve_interval")
                 .takes_value(true)
                 .about("Interval at which stats should be retrieved from interfaces \nDefault : 200 milliseconds"))
        .get_matches();

    let url = matches.value_of("url").unwrap_or("http://127.0.0.1:9200/");
    println!("The elastic url used is: {}", url);

    let index = matches.value_of("index").unwrap_or("ifaces-stats-");
    println!("The elastic index used is: {}", index);

    let binary = matches.value_of("binary").unwrap_or("ifstat");
    println!("The binary used is : {}", binary);

    let send_interval_str = matches.value_of("send_interval").unwrap_or("300");
    let mut send_interval = 300;
    match send_interval_str.parse::<i64>() {
        Ok(n) => send_interval = n,
        Err(_) => println!("That's not a number!"),
    }
    println!("Send interval to Elastic is {}.", send_interval);

    let retrieve_interval_str = matches.value_of("retrieve_interval").unwrap_or("200");
    let mut retrieve_interval = 200;
    match retrieve_interval_str.parse::<i64>() {
        Ok(n) => retrieve_interval = n,
        Err(_) => println!("That's not a number!"),
    }
    println!("Retrieve stats every {} ms.", retrieve_interval);

    let interfaces = matches.value_of("interfaces").unwrap_or("all");
    println!("The interfaces that will be monitored : {}", interfaces);
    
    let ifaces_vec: Vec<&str>;
    if interfaces != "all" {
        let split = interfaces.split(',');
        ifaces_vec = split.collect();
    } else {
        let result = get_ifaces();
        println!("The interfaces that will be monitored : {:#?}", result);
    }

    let output = if cfg!(target_os = "windows") {
        Command::new("cmd")
                .args(&["/C", "echo hello"])
                .output()
                .expect("failed to execute process")
    } else {
        Command::new("../ifstat-nxos.bin")
                .arg("-a")
                .arg("-s")
                .arg("-e")
                .arg("-z")
                .arg("-j")
                .arg("-p")
                .arg("tun0")
                .output()
                .expect("failed to execute process")
    };

    println!("stdout: {}", String::from_utf8_lossy(&output.stdout));

    let parsed = json::parse(&String::from_utf8_lossy(&output.stdout)).unwrap();
    println!("parsed: {}", parsed["kernel"]["tun0"]);
     
}
