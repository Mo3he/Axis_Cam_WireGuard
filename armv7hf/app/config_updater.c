/**
 * ACAP parameter bridge for the WireGuard userspace VPN.
 *
 * Responsibilities:
 *  1. Read WireGuard parameters from the ACAP parameter store (axparameter).
 *  2. Write them to CONFIG_FILE so the Go binary can read them.
 *  3. Launch the Go binary (wireguard-userspace) as a child process.
 *  4. On any parameter change: rewrite CONFIG_FILE and send SIGUSR1 to the
 *     child so it reloads without dropping the tunnel unnecessarily.
 *  5. Watchdog: if the child exits unexpectedly, restart it.
 *
 * Runs as the unprivileged 'sdk' ACAP user — no root or CAP_NET_ADMIN needed.
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
#include <signal.h>

#define APP_NAME        "wireguardconfig"
#define CONFIG_FILE     "/usr/local/packages/wireguardconfig/config.txt"
#define WG_BINARY       "/usr/local/packages/wireguardconfig/lib/wireguard-userspace"

static pid_t wg_pid = -1;

/* ── child process management ─────────────────────────────────────────────── */

static void stop_wireguard(void) {
    if (wg_pid <= 0)
        return;

    if (kill(wg_pid, SIGTERM) == 0)
        waitpid(wg_pid, NULL, 0);

    wg_pid = -1;
}

static void start_wireguard(void) {
    stop_wireguard();

    pid_t pid = fork();
    if (pid < 0) {
        syslog(LOG_ERR, "fork failed: %s", strerror(errno));
        return;
    }
    if (pid == 0) {
        /* child */
        execl(WG_BINARY, "wireguard-userspace", CONFIG_FILE, NULL);
        syslog(LOG_ERR, "execl %s failed: %s", WG_BINARY, strerror(errno));
        _exit(1);
    }
    wg_pid = pid;
    syslog(LOG_INFO, "wireguard-userspace started (pid %d)", wg_pid);
}

static void reload_wireguard(void) {
    if (wg_pid > 0 && kill(wg_pid, 0) == 0) {
        /* process alive — ask it to reload */
        kill(wg_pid, SIGUSR1);
    } else {
        /* not running (first start or crashed) */
        start_wireguard();
    }
}

/* Watchdog: check the child every 60 s and restart if it has died. */
static gboolean watchdog_cb(gpointer G_GNUC_UNUSED data) {
    if (wg_pid > 0) {
        int status;
        pid_t ret = waitpid(wg_pid, &status, WNOHANG);
        if (ret == wg_pid) {
            syslog(LOG_WARNING, "wireguard-userspace exited (status %d), restarting",
                   WEXITSTATUS(status));
            wg_pid = -1;
            start_wireguard();
        }
    }
    return G_SOURCE_CONTINUE;
}

/* ── config file ───────────────────────────────────────────────────────────── */

static void update_config_file(AXParameter *handle) {
    GError *error = NULL;
    gchar *private_key    = NULL;
    gchar *listen_port    = NULL;
    gchar *endpoint       = NULL;
    gchar *peer_public_key = NULL;
    gchar *allowed_ips    = NULL;
    gchar *client_ip      = NULL;

    /* helper macro: get param, fall back to default on error */
#define GET_PARAM(name, dest, def) \
    if (!ax_parameter_get(handle, name, &dest, &error)) { \
        if (error) { g_error_free(error); error = NULL; } \
        dest = g_strdup(def); \
    }

    GET_PARAM("PrivateKey",   private_key,     "")
    GET_PARAM("ListenPort",   listen_port,      "")
    GET_PARAM("Endpoint",     endpoint,         "")
    GET_PARAM("PeerPublicKey", peer_public_key, "")
    GET_PARAM("AllowedIPs",   allowed_ips,      "0.0.0.0/0")
    GET_PARAM("ClientIP",     client_ip,        "10.0.0.2/24")
#undef GET_PARAM

    FILE *f = fopen(CONFIG_FILE, "w");
    if (f) {
        fprintf(f, "private_key=%s\n",     private_key     ? private_key     : "");
        fprintf(f, "listen_port=%s\n",     listen_port     ? listen_port     : "");
        fprintf(f, "endpoint=%s\n",        endpoint        ? endpoint        : "");
        fprintf(f, "peer_public_key=%s\n", peer_public_key ? peer_public_key : "");
        fprintf(f, "allowed_ips=%s\n",     allowed_ips     ? allowed_ips     : "0.0.0.0/0");
        fprintf(f, "client_ip=%s\n",       client_ip       ? client_ip       : "10.0.0.2/24");
        fclose(f);
        chmod(CONFIG_FILE, 0600);
        syslog(LOG_INFO, "config updated (private_key=%s endpoint=%s)",
               (private_key && *private_key) ? "(set)" : "(empty)",
               endpoint ? endpoint : "(empty)");
    } else {
        syslog(LOG_ERR, "cannot open config file: %s", strerror(errno));
    }

    g_free(private_key);
    g_free(listen_port);
    g_free(endpoint);
    g_free(peer_public_key);
    g_free(allowed_ips);
    g_free(client_ip);
}

/* ── ACAP parameter callback ───────────────────────────────────────────────── */

static void parameter_changed(const gchar *name, const gchar *value,
                               gpointer handle_void_ptr) {
    AXParameter *handle = handle_void_ptr;

    /* strip "root.wireguardconfig." prefix for the log */
    const char *short_name = name;
    const char *prefix = "root." APP_NAME ".";
    if (strncmp(name, prefix, strlen(prefix)) == 0)
        short_name = name + strlen(prefix);

    bool sensitive = strstr(short_name, "Key") != NULL;
    syslog(LOG_INFO, "parameter changed: %s = %s",
           short_name, sensitive ? "(sensitive)" : value);

    update_config_file(handle);
    reload_wireguard();
}

/* ── signal handler ────────────────────────────────────────────────────────── */

static gboolean signal_handler(gpointer loop) {
    syslog(LOG_INFO, "stopping");
    stop_wireguard();
    g_main_loop_quit((GMainLoop *)loop);
    return G_SOURCE_REMOVE;
}

/* ── main ──────────────────────────────────────────────────────────────────── */

int main(void) {
    GError *error = NULL;

    openlog(APP_NAME, LOG_PID, LOG_USER);
    syslog(LOG_INFO, "starting");

    AXParameter *handle = ax_parameter_new(APP_NAME, &error);
    if (!handle) {
        syslog(LOG_ERR, "ax_parameter_new: %s",
               error ? error->message : "unknown");
        if (error) g_error_free(error);
        return 1;
    }

    update_config_file(handle);
    start_wireguard();

    /* Register callbacks for every parameter */
    const char *params[] = {
        "PrivateKey", "ListenPort", "Endpoint",
        "PeerPublicKey", "AllowedIPs", "ClientIP",
    };
    for (size_t i = 0; i < sizeof(params) / sizeof(params[0]); i++) {
        if (!ax_parameter_register_callback(handle, params[i],
                                            parameter_changed, handle, &error)) {
            syslog(LOG_WARNING, "register callback %s: %s",
                   params[i], error ? error->message : "unknown");
            if (error) { g_error_free(error); error = NULL; }
        }
    }

    GMainLoop *loop = g_main_loop_new(NULL, FALSE);
    g_unix_signal_add(SIGTERM, signal_handler, loop);
    g_unix_signal_add(SIGINT,  signal_handler, loop);

    /* watchdog every 60 s */
    g_timeout_add_seconds(60, watchdog_cb, NULL);

    syslog(LOG_INFO, "running — waiting for parameter changes");
    g_main_loop_run(loop);

    g_main_loop_unref(loop);
    ax_parameter_free(handle);
    return 0;
}
