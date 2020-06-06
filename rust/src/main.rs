//#[macro_use]
extern crate json;

use clap::{Arg, App};
use std::process::Command;

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
        let output = Command::new("ip")
                .arg("address")
                .arg("show")
                .arg("up")
                .arg("|")
                .arg("grep")
                .arg("mtu")
                .output()
                .expect("failed to execute process");
        println!("stdout: {}", String::from_utf8_lossy(&output.stdout));
        /*
Shows up to the 10th biggest files and subdirectories in the current working directory. It is equivalent to running: du -ah . | sort -hr | head -n 10.

Commands represent a process. Output of a child process is captured with a Stdio::piped between parent and child.

use error_chain::error_chain;

use std::process::{Command, Stdio};

error_chain! {
    foreign_links {
        Io(std::io::Error);
        Utf8(std::string::FromUtf8Error);
    }
}

fn main() -> Result<()> {
    let directory = std::env::current_dir()?;
    let mut du_output_child = Command::new("du")
        .arg("-ah")
        .arg(&directory)
        .stdout(Stdio::piped())
        .spawn()?;

    if let Some(du_output) = du_output_child.stdout.take() {
        let mut sort_output_child = Command::new("sort")
            .arg("-hr")
            .stdin(du_output)
            .stdout(Stdio::piped())
            .spawn()?;

        du_output_child.wait()?;

        if let Some(sort_output) = sort_output_child.stdout.take() {
            let head_output_child = Command::new("head")
                .args(&["-n", "10"])
                .stdin(sort_output)
                .stdout(Stdio::piped())
                .spawn()?;

            let head_stdout = head_output_child.wait_with_output()?;

            sort_output_child.wait()?;

            println!(
                "Top 10 biggest files and directories in '{}':\n{}",
                directory.display(),
                String::from_utf8(head_stdout.stdout).unwrap()
            );
        }
    }

    Ok(())
}
*/
        
        
    }
    //println!("The interfaces that will be monitored : {:#?}", ifaces_vec);

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
