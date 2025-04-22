#!/bin/sh

# Simple script to start WireGuard with custom configuration
CONFIG_FILE="/usr/local/packages/wireguardconfig/config.txt"
WIREGUARD_GO_PATH="/usr/local/packages/wireguardconfig/lib/wireguard-go"
WG_PATH="/usr/local/packages/wireguardconfig/lib/wg"
CONFIG_DIR="/usr/local/packages/wireguardconfig/config"
INTERFACE_NAME="wg0"

# Ensure config directory exists
mkdir -p "$CONFIG_DIR"

# Log to syslog
logger -t "wireguard_script" "Starting WireGuard VPN service"

# Set execute permissions
chmod 755 $WIREGUARD_GO_PATH
chmod 755 $WG_PATH

# Read configuration (if exists)
PRIVATE_KEY=""
LISTEN_PORT=""
ENDPOINT=""
PEER_PUBLIC_KEY=""
ALLOWED_IPS="0.0.0.0/0"
CLIENT_IP=""

if [ -f "$CONFIG_FILE" ]; then
    logger -t "wireguard_script" "Reading configuration from $CONFIG_FILE"
    while IFS= read -r line || [ -n "$line" ]; do
        # Skip empty lines and comments
        [ -z "$line" ] && continue
        case "$line" in
            \#*) continue ;; # Skip comments
        esac

        key="${line%%=*}"
        value="${line#*=}"

        # Strip quotes and spaces
        key=$(echo "$key" | sed -e 's/^"//' -e 's/"$//' -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')
        value=$(echo "$value" | sed -e 's/^"//' -e 's/"$//' -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')

        case "$key" in
            "private_key") PRIVATE_KEY="$value" ;;
            "listen_port") LISTEN_PORT="$value" ;;
            "endpoint") ENDPOINT="$value" ;;
            "peer_public_key") PEER_PUBLIC_KEY="$value" ;;
            "allowed_ips") ALLOWED_IPS="$value" ;;
            "client_ip") CLIENT_IP="$value" ;;
        esac
    done < "$CONFIG_FILE"
fi

# Create private key file if provided in config
if [ -n "$PRIVATE_KEY" ]; then
    echo "$PRIVATE_KEY" > "$CONFIG_DIR/private.key"
    chmod 600 "$CONFIG_DIR/private.key"
fi

# Start wireguard-go in userspace mode
logger -t "wireguard_script" "Starting wireguard-go for interface $INTERFACE_NAME"
$WIREGUARD_GO_PATH $INTERFACE_NAME

# Wait for wireguard interface to initialize
sleep 2

# Configure WireGuard interface
if [ -n "$PRIVATE_KEY" ] && [ -n "$PEER_PUBLIC_KEY" ]; then
    logger -t "wireguard_script" "Configuring WireGuard interface"
    
    # Set client IP address if provided
    if [ -n "$CLIENT_IP" ]; then
        logger -t "wireguard_script" "Setting IP address: $CLIENT_IP"
        ip address add $CLIENT_IP dev $INTERFACE_NAME
    fi
    
    # Build the WireGuard configuration command
    WG_CMD="$WG_PATH set $INTERFACE_NAME private-key $CONFIG_DIR/private.key"
    
    if [ -n "$LISTEN_PORT" ]; then
        WG_CMD="$WG_CMD listen-port $LISTEN_PORT"
    fi
    
    if [ -n "$PEER_PUBLIC_KEY" ]; then
        WG_CMD="$WG_CMD peer $PEER_PUBLIC_KEY"
        
        if [ -n "$ALLOWED_IPS" ]; then
            WG_CMD="$WG_CMD allowed-ips $ALLOWED_IPS"
        fi
        
        if [ -n "$ENDPOINT" ]; then
            WG_CMD="$WG_CMD endpoint $ENDPOINT"
        fi
        
        # Add PersistentKeepalive (e.g., every 25 seconds)
        PERSISTENT_KEEPALIVE=25
        WG_CMD="$WG_CMD persistent-keepalive $PERSISTENT_KEEPALIVE"
    fi
    
    # Execute WireGuard configuration
    logger -t "wireguard_script" "Running: $WG_CMD"
    eval $WG_CMD
    
    # Bring up the interface
    ip link set up dev $INTERFACE_NAME
    
    # Log the interface status
    $WG_PATH show
fi

logger -t "wireguard_script" "WireGuard VPN is running"
logger -t "wireguard_script" "To change settings, modify parameters in ACAP web interface"

# Keep the script running to maintain the process
while true; do
    # Check if the interface is still up every 60 seconds
    sleep 60
    if ! ip link show $INTERFACE_NAME > /dev/null 2>&1; then
        logger -t "wireguard_script" "WireGuard interface is down, restarting..."
        $WIREGUARD_GO_PATH $INTERFACE_NAME
        sleep 2
        
        # Reconfigure the interface
        eval $WG_CMD
        ip link set up dev $INTERFACE_NAME
    fi

done