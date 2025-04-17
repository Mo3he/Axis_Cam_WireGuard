/**
 * Simple file-based configuration updater for WireGuard
 * Based on Tailscale implementation but adapted for WireGuard parameters
 */
#include <axsdk/axparameter.h>
#include <glib-unix.h>
#include <stdbool.h>
#include <syslog.h>
#include <string.h>
#include <stdlib.h>
#include <stdio.h>
#include <unistd.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <sys/stat.h>
#include <fcntl.h>
#include <errno.h>

#define APP_NAME "wireguardconfig"
#define CONFIG_FILE "/usr/local/packages/wireguardconfig/config.txt"
#define SCRIPT_PATH "/usr/local/packages/wireguardconfig/start_wireguard.sh"
#define SCRIPT_SOURCE "/usr/local/packages/wireguardconfig/lib/start_wireguard.sh"

static gboolean signal_handler(gpointer loop) {
    g_main_loop_quit((GMainLoop*)loop);
    syslog(LOG_INFO, "WireGuard configuration updater stopping.");
    return G_SOURCE_REMOVE;
}

// Copy script from lib folder to main directory
static void copy_script_file(void) {
    char buffer[4096];
    ssize_t bytes_read, bytes_written;
    int source_fd, dest_fd;
    
    syslog(LOG_INFO, "Copying script from %s to %s", SCRIPT_SOURCE, SCRIPT_PATH);
    
    // Open source file
    source_fd = open(SCRIPT_SOURCE, O_RDONLY);
    if (source_fd < 0) {
        syslog(LOG_ERR, "Failed to open source script: %s", strerror(errno));
        return;
    }
    
    // Open destination file (create if doesn't exist, truncate if exists)
    dest_fd = open(SCRIPT_PATH, O_WRONLY | O_CREAT | O_TRUNC, 0755);
    if (dest_fd < 0) {
        syslog(LOG_ERR, "Failed to open destination script: %s", strerror(errno));
        close(source_fd);
        return;
    }
    
    // Copy the file
    while ((bytes_read = read(source_fd, buffer, sizeof(buffer))) > 0) {
        bytes_written = write(dest_fd, buffer, bytes_read);
        if (bytes_written != bytes_read) {
            syslog(LOG_ERR, "Error writing to destination file: %s", strerror(errno));
            close(source_fd);
            close(dest_fd);
            return;
        }
    }
    
    // Close file descriptors
    close(source_fd);
    close(dest_fd);
    
    // Make the script executable
    if (chmod(SCRIPT_PATH, 0755) != 0) {
        syslog(LOG_ERR, "Failed to make script executable: %s", strerror(errno));
        return;
    }
    
    syslog(LOG_INFO, "Script copied and made executable successfully");
}

// Execute the WireGuard script
static void start_wireguard(void) {
    syslog(LOG_INFO, "Starting WireGuard VPN script");
    
    // Check if script exists, if not, copy it
    struct stat st;
    if (stat(SCRIPT_PATH, &st) != 0) {
        syslog(LOG_INFO, "Script not found at %s, copying from lib folder", SCRIPT_PATH);
        copy_script_file();
    }
    
    // Fork and execute the script
    pid_t pid = fork();
    if (pid < 0) {
        syslog(LOG_ERR, "Failed to fork for WireGuard script: %s", strerror(errno));
        return;
    } else if (pid == 0) {
        // Child process - execute the script
        execl(SCRIPT_PATH, "start_wireguard.sh", NULL);
        
        // If we get here, execl failed
        syslog(LOG_ERR, "Failed to execute WireGuard script: %s", strerror(errno));
        _exit(1);
    }
    
    syslog(LOG_INFO, "WireGuard script started with PID: %d", pid);
}

// Update the configuration file with current parameter values
static void update_config_file(AXParameter* handle) {
    GError* error = NULL;
    gchar* private_key = NULL;
    gchar* listen_port = NULL;
    gchar* endpoint = NULL;
    gchar* peer_public_key = NULL;
    gchar* allowed_ips = NULL;
    gchar* client_ip = NULL;
    FILE* file;
    
    // Get parameter values
    if (!ax_parameter_get(handle, "PrivateKey", &private_key, &error)) {
        syslog(LOG_ERR, "Failed to get PrivateKey: %s", 
               error ? error->message : "unknown error");
        if (error) g_error_free(error);
        error = NULL;
        private_key = g_strdup("");
    }
    
    if (!ax_parameter_get(handle, "ListenPort", &listen_port, &error)) {
        syslog(LOG_ERR, "Failed to get ListenPort: %s", 
               error ? error->message : "unknown error");
        if (error) g_error_free(error);
        error = NULL;
        listen_port = g_strdup("51820");  // Default WireGuard port
    }
    
    if (!ax_parameter_get(handle, "Endpoint", &endpoint, &error)) {
        syslog(LOG_ERR, "Failed to get Endpoint: %s", 
               error ? error->message : "unknown error");
        if (error) g_error_free(error);
        error = NULL;
        endpoint = g_strdup("");
    }
    
    if (!ax_parameter_get(handle, "PeerPublicKey", &peer_public_key, &error)) {
        syslog(LOG_ERR, "Failed to get PeerPublicKey: %s", 
               error ? error->message : "unknown error");
        if (error) g_error_free(error);
        error = NULL;
        peer_public_key = g_strdup("");
    }
    
    if (!ax_parameter_get(handle, "AllowedIPs", &allowed_ips, &error)) {
        syslog(LOG_ERR, "Failed to get AllowedIPs: %s", 
               error ? error->message : "unknown error");
        if (error) g_error_free(error);
        error = NULL;
        allowed_ips = g_strdup("0.0.0.0/0");  // Default to route all traffic
    }
    
    if (!ax_parameter_get(handle, "ClientIP", &client_ip, &error)) {
        syslog(LOG_ERR, "Failed to get ClientIP: %s", 
               error ? error->message : "unknown error");
        if (error) g_error_free(error);
        error = NULL;
        client_ip = g_strdup("10.0.0.2/24");  // Default client IP
    }
    
    // Write to config file
    file = fopen(CONFIG_FILE, "w");
    if (file) {
        fprintf(file, "private_key=%s\n", private_key ? private_key : "");
        fprintf(file, "listen_port=%s\n", listen_port ? listen_port : "51820");
        fprintf(file, "endpoint=%s\n", endpoint ? endpoint : "");
        fprintf(file, "peer_public_key=%s\n", peer_public_key ? peer_public_key : "");
        fprintf(file, "allowed_ips=%s\n", allowed_ips ? allowed_ips : "0.0.0.0/0");
        fprintf(file, "client_ip=%s\n", client_ip ? client_ip : "10.0.0.2/24");
        fclose(file);
        
        // Set restrictive permissions since this file contains a private key
        chmod(CONFIG_FILE, 0600);
        
        syslog(LOG_INFO, "Updated configuration file with new parameters");
        syslog(LOG_INFO, "private_key=%s", private_key && strlen(private_key) > 0 ? "(set)" : "(empty)");
        syslog(LOG_INFO, "listen_port=%s", listen_port ? listen_port : "51820");
        syslog(LOG_INFO, "endpoint=%s", endpoint ? endpoint : "(empty)");
        syslog(LOG_INFO, "peer_public_key=%s", peer_public_key && strlen(peer_public_key) > 0 ? "(set)" : "(empty)");
        syslog(LOG_INFO, "allowed_ips=%s", allowed_ips ? allowed_ips : "0.0.0.0/0");
        syslog(LOG_INFO, "client_ip=%s", client_ip ? client_ip : "10.0.0.2/24");
    } else {
        syslog(LOG_ERR, "Failed to open config file for writing");
    }
    
    // Clean up
    g_free(private_key);
    g_free(listen_port);
    g_free(endpoint);
    g_free(peer_public_key);
    g_free(allowed_ips);
    g_free(client_ip);
}

// Handle parameter changes
static void parameter_changed(const gchar* name, const gchar* value, gpointer handle_void_ptr) {
    AXParameter* handle = handle_void_ptr;
    
    // Extract simple parameter name from the fully qualified name
    const char* simple_name = name;
    const char* prefix = "root." APP_NAME ".";
    if (strncmp(name, prefix, strlen(prefix)) == 0) {
        simple_name = name + strlen(prefix);
    }
    
    syslog(LOG_INFO, "Parameter changed: %s = %s", simple_name, 
           (strstr(simple_name, "PrivateKey") || strstr(simple_name, "PeerPublicKey")) ? "(sensitive value)" : value);
    
    // Update config file whenever any parameter changes
    update_config_file(handle);
    
    // Restart WireGuard to apply the new settings
    start_wireguard();
}

int main(void) {
    GError* error = NULL;
    GMainLoop* loop = NULL;

    // Open syslog for logging
    openlog(APP_NAME, LOG_PID, LOG_USER);
    syslog(LOG_INFO, "WireGuard config updater starting");

    // Initialize parameter handling
    AXParameter* handle = ax_parameter_new(APP_NAME, &error);
    if (handle == NULL) {
        syslog(LOG_ERR, "Failed to initialize parameters: %s", 
               error ? error->message : "unknown error");
        if (error) g_error_free(error);
        exit(1);
    }

    // Ensure script is copied from lib folder
    copy_script_file();

    // Create initial config file
    update_config_file(handle);
    
    // Start WireGuard VPN script
    start_wireguard();

    // Register for parameter changes - all parameters
    const char* params[] = {
        "PrivateKey", "ListenPort", "Endpoint", 
        "PeerPublicKey", "AllowedIPs", "ClientIP"
    };
    
    for (size_t i = 0; i < sizeof(params)/sizeof(params[0]); i++) {
        // Try regular parameter name
        if (!ax_parameter_register_callback(handle, params[i], parameter_changed, handle, &error)) {
            syslog(LOG_ERR, "Failed to register %s callback: %s", 
                   params[i], error ? error->message : "unknown error");
            if (error) {
                g_error_free(error);
                error = NULL;
            }
            
            // Try with fully qualified name as fallback
            char full_name[256];
            snprintf(full_name, sizeof(full_name), "root.%s.%s", APP_NAME, params[i]);
            if (!ax_parameter_register_callback(handle, full_name, parameter_changed, handle, NULL)) {
                syslog(LOG_INFO, "Fallback %s registration failed (this may be normal)", params[i]);
            }
        }
    }

    // Set up main loop
    loop = g_main_loop_new(NULL, FALSE);
    g_unix_signal_add(SIGTERM, signal_handler, loop);
    g_unix_signal_add(SIGINT, signal_handler, loop);
    
    syslog(LOG_INFO, "WireGuard config updater running. Waiting for parameter changes...");
    g_main_loop_run(loop);

    // Clean up
    g_main_loop_unref(loop);
    ax_parameter_free(handle);
    
    return 0;
}