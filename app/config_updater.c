// Copyright (C) 2024  Mo3he
// SPDX-License-Identifier: GPL-3.0-or-later

/**
 * ACAP parameter bridge for the WireGuard userspace VPN.
 *
 * Responsibilities:
 *  1. Read WireGuard parameters from the ACAP parameter store (axparameter).
 *  2. Write them to CONFIG_FILE so the Go binary can read them.
 *  3. Launch the Go binary (wireguard-userspace) as a child process.
 *  4. On any parameter change: rewrite CONFIG_FILE and do a full stop+restart
 *     of the child so ports are cleanly released and new config is picked up.
 *     Rapid changes within 300 ms are coalesced into a single restart.
 *  5. Watchdog: if the child exits unexpectedly, restart it.
 *
 * Runs as the unprivileged 'sdk' ACAP user — no root required.
 */

#include <axsdk/axparameter.h>
#include <errno.h>
#include <fcntl.h>
#include <glib-unix.h>
#include <signal.h>
#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <syslog.h>
#include <unistd.h>

#define APP_NAME "wireguardconfig"
#define CONFIG_FILE "/usr/local/packages/wireguardconfig/config.txt"
#define WG_BINARY "/usr/local/packages/wireguardconfig/lib/wireguard-userspace"

static pid_t wg_pid = -1;
static guint reload_timer_id = 0;

/* ── cached config (updated in-place from callback value args) ─────────────── */

static char *cfg_private_key = NULL;
static char *cfg_listen_port = NULL;
static char *cfg_endpoint = NULL;
static char *cfg_peer_pub_key = NULL;
static char *cfg_allowed_ips = NULL;
static char *cfg_client_ip = NULL;
static char *cfg_http_proxy_port = NULL;
static char *cfg_socks5_port = NULL;

static void cache_set(char **field, const char *value) {
    if (!value)
        return; /* NULL → keep existing cached value */
    free(*field);
    *field = strdup(value);
}

static const char *cache_get(char **field, const char *fallback) {
    return (*field && **field) ? *field : fallback;
}

/* ── child process management ─────────────────────────────────────────────── */

static void stop_proxy(void) {
    if (wg_pid <= 0)
        return;

    kill(wg_pid, SIGTERM);

    /* Poll for up to 3 s so we never block the glib main loop forever. */
    for (int i = 0; i < 30; i++) {
        int status;
        if (waitpid(wg_pid, &status, WNOHANG) == wg_pid) {
            wg_pid = -1;
            return;
        }
        usleep(100000); /* 100 ms */
    }

    /* Still alive after 3 s — force-kill. */
    syslog(LOG_WARNING, "wireguard-userspace did not exit in 3 s, sending SIGKILL");
    kill(wg_pid, SIGKILL);
    waitpid(wg_pid, NULL, 0);
    wg_pid = -1;
}

static void start_proxy(void) {
    stop_proxy();
    pid_t pid = fork();
    if (pid < 0) {
        syslog(LOG_ERR, "fork failed: %s", strerror(errno));
        return;
    }
    if (pid == 0) {
        execl(WG_BINARY, "wireguard-userspace", CONFIG_FILE, NULL);
        syslog(LOG_ERR, "execl %s failed: %s", WG_BINARY, strerror(errno));
        _exit(1);
    }
    wg_pid = pid;
    syslog(LOG_INFO, "wireguard-userspace started (pid %d)", wg_pid);
}

/* ── watchdog ─────────────────────────────────────────────────────────────── */

static gboolean watchdog_cb(gpointer G_GNUC_UNUSED data) {
    if (wg_pid > 0) {
        int status;
        pid_t ret = waitpid(wg_pid, &status, WNOHANG);
        if (ret == wg_pid) {
            syslog(LOG_WARNING, "wireguard-userspace exited (status %d), restarting", WEXITSTATUS(status));
            wg_pid = -1;
            start_proxy();
        }
    }
    return G_SOURCE_CONTINUE;
}

/* ── config file ──────────────────────────────────────────────────────────── */

/* Read all params from the store — safe to call any time outside a callback. */
static void load_config_cache(AXParameter *handle) {
    GError *error = NULL;
    gchar *val = NULL;

    // clang-format off
#define LOAD(name, field) \
    val = NULL; error = NULL; \
    if (ax_parameter_get(handle, name, &val, &error)) { \
        free(field); field = val ? strdup(val) : strdup(""); \
        g_free(val); val = NULL; \
    } else { \
        syslog(LOG_WARNING, "ax_parameter_get %s failed: %s", name, \
               error ? error->message : "unknown"); \
        if (error) { g_error_free(error); error = NULL; } \
    }

    LOAD("PrivateKey",         cfg_private_key)
    LOAD("ListenPort",         cfg_listen_port)
    LOAD("Endpoint",           cfg_endpoint)
    LOAD("PeerPublicKey",      cfg_peer_pub_key)
    LOAD("AllowedIPs",         cfg_allowed_ips)
    LOAD("ClientIP",           cfg_client_ip)
    LOAD("HTTPProxyPort",      cfg_http_proxy_port)
    LOAD("OutboundSOCKS5Port", cfg_socks5_port)
#undef LOAD
    // clang-format on
}

static void write_config_file(void) {
    FILE *f = fopen(CONFIG_FILE, "w");
    if (!f) {
        syslog(LOG_ERR, "cannot open config file: %s", strerror(errno));
        return;
    }
    // clang-format off
    fprintf(f, "private_key=%s\n",         cache_get(&cfg_private_key,  ""));
    fprintf(f, "listen_port=%s\n",         cache_get(&cfg_listen_port,  "51820"));
    fprintf(f, "endpoint=%s\n",            cache_get(&cfg_endpoint,     ""));
    fprintf(f, "peer_public_key=%s\n",     cache_get(&cfg_peer_pub_key, ""));
    fprintf(f, "allowed_ips=%s\n",         cache_get(&cfg_allowed_ips,  "0.0.0.0/0"));
    fprintf(f, "client_ip=%s\n",           cache_get(&cfg_client_ip,    "10.0.0.2/24"));
    fprintf(f, "http_proxy_port=%s\n",     cache_get(&cfg_http_proxy_port, "8080"));
    fprintf(f, "outbound_socks5_port=%s\n",cache_get(&cfg_socks5_port,  "1080"));
    // clang-format on
    fclose(f);
    chmod(CONFIG_FILE, 0600);
    syslog(LOG_INFO, "config updated (endpoint=%s)", cache_get(&cfg_endpoint, "(empty)"));
}

/* ── ACAP parameter callback ──────────────────────────────────────────────── */

/* The AXParameter handle stored at startup — used for the fallback read. */
static AXParameter *g_ax_handle = NULL;

static gboolean debounced_restart(gpointer G_GNUC_UNUSED data) {
    reload_timer_id = 0;

    /* Always re-read ALL params from the store at this point.
     * By 300 ms after the callback the Axis parameter write is complete,
     * so ax_parameter_get is instant (no lock contention).  This is the
     * authoritative refresh regardless of what the callback value arg held. */
    if (g_ax_handle) {
        load_config_cache(g_ax_handle);
    }

    write_config_file();
    syslog(LOG_INFO, "restarting wireguard-userspace with new config");
    stop_proxy();
    start_proxy();
    return G_SOURCE_REMOVE;
}

static void parameter_changed(const gchar *name, const gchar *value, gpointer G_GNUC_UNUSED handle_void_ptr) {
    /* Use the last component after '.' as the short name.
     * The full name casing varies by firmware (e.g. "root.Wireguardconfig.Endpoint"
     * vs "root.wireguardconfig.Endpoint") so we never rely on the prefix. */
    const char *dot = strrchr(name, '.');
    const char *short_name = dot ? dot + 1 : name;

    syslog(LOG_INFO, "parameter changed: %s value=%s (raw name: %s)", short_name, value ? value : "(null)", name);

    /* Cache the new value from the callback argument if non-NULL. */
    // clang-format off
    if      (strcmp(short_name, "PrivateKey")         == 0) cache_set(&cfg_private_key,      value);
    else if (strcmp(short_name, "ListenPort")         == 0) cache_set(&cfg_listen_port,      value);
    else if (strcmp(short_name, "Endpoint")           == 0) cache_set(&cfg_endpoint,         value);
    else if (strcmp(short_name, "PeerPublicKey")      == 0) cache_set(&cfg_peer_pub_key,     value);
    else if (strcmp(short_name, "AllowedIPs")         == 0) cache_set(&cfg_allowed_ips,      value);
    else if (strcmp(short_name, "ClientIP")           == 0) cache_set(&cfg_client_ip,        value);
    else if (strcmp(short_name, "HTTPProxyPort")      == 0) cache_set(&cfg_http_proxy_port,  value);
    else if (strcmp(short_name, "OutboundSOCKS5Port") == 0) cache_set(&cfg_socks5_port,      value);
    else syslog(LOG_WARNING, "unknown parameter: %s (raw: %s)", short_name, name);
    // clang-format on

    /* Coalesce all 6 saves into one restart 300 ms after the last change. */
    if (reload_timer_id)
        g_source_remove(reload_timer_id);
    reload_timer_id = g_timeout_add(300, debounced_restart, NULL);
}

/* ── signal handler ───────────────────────────────────────────────────────── */

static gboolean signal_handler(gpointer loop) {
    syslog(LOG_INFO, "stopping");
    stop_proxy();
    g_main_loop_quit((GMainLoop *)loop);
    return G_SOURCE_REMOVE;
}

/* ── main ─────────────────────────────────────────────────────────────────── */

int main(void) {
    GError *error = NULL;

    openlog(APP_NAME, LOG_PID, LOG_USER);
    syslog(LOG_INFO, "starting");

    AXParameter *handle = ax_parameter_new(APP_NAME, &error);
    if (!handle) {
        syslog(LOG_ERR, "ax_parameter_new: %s", error ? error->message : "unknown");
        if (error)
            g_error_free(error);
        return 1;
    }
    g_ax_handle = handle;

    load_config_cache(handle);
    write_config_file();
    start_proxy();

    const char *params[] = {
        "PrivateKey",
        "ListenPort",
        "Endpoint",
        "PeerPublicKey",
        "AllowedIPs",
        "ClientIP",
        "HTTPProxyPort",
        "OutboundSOCKS5Port"};
    for (size_t i = 0; i < sizeof(params) / sizeof(params[0]); i++) {
        if (!ax_parameter_register_callback(handle, params[i], parameter_changed, handle, &error)) {
            syslog(LOG_WARNING, "register callback %s: %s", params[i], error ? error->message : "unknown");
            if (error) {
                g_error_free(error);
                error = NULL;
            }
        }
    }

    GMainLoop *loop = g_main_loop_new(NULL, FALSE);
    g_unix_signal_add(SIGTERM, signal_handler, loop);
    g_unix_signal_add(SIGINT, signal_handler, loop);
    g_timeout_add_seconds(60, watchdog_cb, NULL);

    syslog(LOG_INFO, "running — waiting for parameter changes");
    g_main_loop_run(loop);

    g_main_loop_unref(loop);
    ax_parameter_free(handle);
    return 0;
}
