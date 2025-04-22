# The WireGuard installer ACAP

This ACAP packages the scripts and files required to install and configure a WireGuard client on Axis Cameras.

Current version 1.0.0

## Please note!

This  ACAP requires root priviliages to run.

## Purpose

Adding a VPN client directly to the camera allows secure remote access to the device without requiring any other equipment or network configuration.
WireGuard achieves this in a secure, simple to setup and easy to use way.

## Links

https://www.wireguard.com/

https://www.axis.com/

## Compatibility

The WireGuard ACAP is compatable with Axis cameras with arm and aarch64 based Soc's.

```
curl --anyauth "*" -u <username>:<password> <device ip>/axis-cgi/basicdeviceinfo.cgi --data "{\"apiVersion\":\"1.0\",\"context\":\"Client defined request ID\",\"method\":\"getAllProperties\"}"
```

where `<device ip>` is the IP address of the Axis device, `<username>` is the root username and `<password>` is the root password. Please
note that you need to enclose your password with quotes (`'`) if it contains special characters.

## Installing

The recommended way to install this ACAP is to use the pre built eap file.
Go to "Apps" on the camera and click "Add app".


## Using the WireGuard ACAP

The WireGuard ACAP will run a script on startup that sets the required permissions and starts the service and app.

You will need a WireGuard server to use the ACAP

Configure the parameters below to connect to your WireGuard VPN server.

Private Key: Your WireGuard private key (keep this secret)

Listen Port: Local UDP port to listen on (default: 51820)

Server Endpoint: Your WireGuard server address and port (e.g., server.example.com:51820)

Peer Public Key: The public key of your WireGuard server

Allowed IPs: IP ranges to route through the VPN (default: 0.0.0.0/0 for all traffic)

Client IP: The IP address for this client on the VPN network (e.g., 10.0.0.2/24)

When uninstalling the ACAP, all changes and files are removed from the camera.

## Updating WireGuard version

The eap files will be updated from time to time and simply installing the new version over the old will update all files.

It's also possible to build and use a locally built image as all necesary files are provided.

Replace binaries "WG" and "wireguard-go" in lib folder with new versions.
Make sure you use the files for the correct Soc.


To build, 
From main directory of the version you want (arm/aarch64)

```
docker build --tag <package name> . 
```
```
docker cp $(docker create <package name>):/opt/app ./build 
```

