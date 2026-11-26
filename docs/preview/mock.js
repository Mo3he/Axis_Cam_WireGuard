// Sample data only. To refresh, copy the current app/html/index.html into this preview and update these fixtures to match the app UI/API; never copy live credentials.
(function () {
  "use strict";

  const app = document.documentElement.dataset.previewApp;
  const fixtures = {
    wireguard: {
      params: [
        "root.WireGuard_VPN.PrivateKey=PREVIEW-ONLY-NOT-A-KEY",
        "root.WireGuard_VPN.ListenPort=51820",
        "root.WireGuard_VPN.Endpoint=vpn.example.invalid:51820",
        "root.WireGuard_VPN.PeerPublicKey=PREVIEW-ONLY-NOT-A-KEY",
        "root.WireGuard_VPN.AllowedIPs=10.66.0.0/24",
        "root.WireGuard_VPN.ClientIP=10.66.0.2/24",
        "root.WireGuard_VPN.HTTPProxyPort=8080",
        "root.WireGuard_VPN.OutboundSOCKS5Port=1080",
        "root.WireGuard_VPN.ForwardPorts=80,443,554",
        "root.WireGuard_VPN.MTU=1420",
        "root.WireGuard_VPN.LocalForwards="
      ].join("\n"),
      log: [
        "Nov 26 10:40:00 camera WireGuard tunnel up ip=192.0.2.10/24 endpoint=vpn.example.invalid:51820",
        "Nov 26 10:40:01 camera HTTP CONNECT proxy ready addr=127.0.0.1:8080",
        "Nov 26 10:40:01 camera Outbound SOCKS5 proxy ready addr=127.0.0.1:1080",
        "Nov 26 10:40:05 camera WireGuard handshake ok"
      ].join("\n")
    },
    wireguardServer: {
      settings: {
        Enabled: "yes", PublicEndpoint: "vpn.example.invalid:51820",
        ListenPort: "51820", Address: "10.66.0.1/24", ClientDNS: "",
        RouteSubnets: "", PeerToPeer: "no", MTU: "1420",
        PrivateKeyIsSet: "yes",
        Peers: "sample-laptop,PREVIEW-ONLY-NOT-A-KEY,10.66.0.2"
      },
      status: {
        state: "running", publicKey: "PREVIEW-ONLY-NOT-A-KEY", listenPort: 51820,
        address: "10.66.0.1/24", cameraIP: "192.0.2.5", lanSubnet: "192.0.2.0/24",
        routeSubnets: [], peerToPeer: false, peerErrors: [],
        peers: [{
          name: "sample-laptop", publicKey: "PREVIEW-ONLY-NOT-A-KEY",
          address: "10.66.0.2", connected: true, lastHandshake: Math.floor(Date.now() / 1000) - 74,
          endpoint: "vpn.example.invalid:51820", rxBytes: 262144, txBytes: 131072
        }]
      },
      log: "Nov 26 10:40:00 camera WireGuard server started (offline preview sample)\nNov 26 10:40:05 camera peer sample-laptop handshake complete"
    },
    zerotier: {
      status: {
        state: "connected", zt_ip: "192.0.2.20", node_id: "SAMPLE-NODE-ID",
        network_id: "0000000000000000", http_port: 8080, socks5_port: 1080,
        gateway: "192.0.2.1",
        routes: [{ target: "192.0.2.0/24", via: "192.0.2.1" }]
      },
      params: [
        "root.ZeroTier_VPN.NetworkID=0000000000000000",
        "root.ZeroTier_VPN.PlanetFile=",
        "root.ZeroTier_VPN.HTTPProxyPort=8080",
        "root.ZeroTier_VPN.SOCKS5ProxyPort=1080",
        "root.ZeroTier_VPN.ForwardPorts=80,443,554",
        "root.ZeroTier_VPN.ManagedGateway="
      ].join("\n"),
      log: "Nov 26 10:40:00 camera ZeroTier VPN is running\nNov 26 10:40:02 camera Node online, ID: SAMPLE-NODE-ID\nNov 26 10:40:03 camera Joining network 0000000000000000\nNov 26 10:40:04 camera Address assigned: 192.0.2.20\nNov 26 10:40:05 camera HTTP CONNECT proxy ready on 127.0.0.1:8080\nNov 26 10:40:05 camera Outbound SOCKS5 proxy ready on 127.0.0.1:1080",
      apps: '<application Name="ZeroTier_VPN" Status="Running"/>'
    },
    netbird: {
      params: [
        "root.NetBird_VPN.ManagementURL=https://management.example.invalid",
        "root.NetBird_VPN.HTTPProxyPort=8080",
        "root.NetBird_VPN.Socks5Port=1080",
        "root.NetBird_VPN.ForwardPorts=80,443,554",
        "root.NetBird_VPN.InboundSocks5Port=1081",
        "root.NetBird_VPN.RoutingMode=off",
        "root.NetBird_VPN.AcceptRoutes=no"
      ].join("\n"),
      status: {
        connected: true, management_connected: true, signal_connected: true,
        overlay_ip: "192.0.2.60/24", proxy_address: "127.0.0.1:8080",
        socks5_address: "127.0.0.1:1080", inbound_socks5_address: "overlay:1081",
        forward_ports: [80, 443, 554], routing_mode: "off", accept_routes: false,
        served_routes: [], peer_routes: [], exit_node: ""
      },
      log: "Nov 26 10:40:00 camera NetBird starting (offline preview sample)\nNov 26 10:40:03 camera Connected to management.example.invalid\nNov 26 10:40:05 camera Signal connected; overlay address assigned"
    },
    openvpn: {
      status: {
        state: "connected", vpn_ip4: "192.0.2.30", http_port: "8080",
        socks_port: "1080", last_error: ""
      },
      params: [
        "root.OpenVPN_VPN.Username=sample-user",
        "root.OpenVPN_VPN.HttpProxyPort=8080",
        "root.OpenVPN_VPN.Socks5Port=1080",
        "root.OpenVPN_VPN.ForwardPorts=80,443,554"
      ].join("\n"),
      log: "Nov 26 10:40:00 camera OpenVPN service starting (offline preview sample)\nNov 26 10:40:04 camera TLS handshake completed\nNov 26 10:40:05 camera Initialization Sequence Completed"
    },
    openconnect: {
      status: {
        state: "connected", vpn_ip: "192.0.2.40", transport: "TLS",
        connected_since: "1795686000", retry_in: 0, last_error: "",
        warning: "", http_port: "28080", socks_port: "21080",
        forward_ports: "80,443,554", session: false, login: null
      },
      settings: {
        Enabled: "yes", Server: "vpn.example.invalid", Gateway: "",
        Username: "sample-user", ServerType: "portal", ReportedOs: "linux",
        HttpProxyPort: "28080", Socks5Port: "21080", ForwardPorts: "80,443,554",
        KeepAliveMinutes: "0", PasswordIsSet: "no", ClientCertIsSet: "no", CaIsSet: "no"
      },
      log: "Nov 26 10:40:00 camera OpenConnect service starting (offline preview sample)\nNov 26 10:40:04 camera TLS tunnel established to vpn.example.invalid\nNov 26 10:40:05 camera VPN address assigned: 192.0.2.40"
    },
    tailcat: {
      settings: {
        ports: [80, 443, 554], autoStart: false, pinAddress: false,
        exitNodeMode: "off", exitNodeAllow: [], allowedClients: [],
        autoStopMode: "idle", autoStopMinutes: 60, derpMapUrl: "", verbose: false
      },
      status: {
        running: true,
        address: "https://preview.invalid/t/SAMPLE-NOT-A-CREDENTIAL",
        pinned: false, startedAt: "2026-11-26T09:40:00Z",
        stopsAt: "2026-11-26T10:40:00Z", exitNode: "", tailcatVersion: "sample",
        clients: [{
          address: "192.0.2.50", active: 1, total: 3,
          lastSeen: "2026-11-26T09:45:00Z"
        }],
        ports: [
          { port: 80, listening: true },
          { port: 443, listening: true },
          { port: 554, listening: true }
        ]
      }
    }
  };

  const sample = fixtures[app];
  if (!sample) return;
  const log = sample.log || "";

  function response(body, type, status) {
    return new Response(body, {
      status: status || 200,
      headers: { "Content-Type": type || "application/json; charset=utf-8" }
    });
  }
  function json(value) {
    return response(JSON.stringify(value));
  }

  window.__previewRequests = [];
  window.__previewBlockedExternal = [];
  window.fetch = function (input, init) {
    const raw = typeof input === "string" ? input : input.url;
    const url = new URL(raw, window.location.href);
    const method = (init && init.method) || (input && input.method) || "GET";
    const path = url.pathname;
    window.__previewRequests.push({ method: method.toUpperCase(), url: url.href });

    if (url.origin !== window.location.origin) {
      if (url.hostname === "api.github.com" && /\/repos\/Mo3he\/Axis_Cam_(WireGuard|ZeroTier)\/releases\/latest$/.test(path)) {
        return Promise.resolve(json({ tag_name: app === "wireguard" ? "v1.2.20" : "v1.16.15", assets: [] }));
      }
      window.__previewBlockedExternal.push(url.href);
      return Promise.resolve(response("External requests are disabled in this offline preview.", "text/plain", 451));
    }

    if (path.endsWith("/axis-cgi/admin/systemlog.cgi")) {
      return Promise.resolve(response(log, "text/plain; charset=utf-8"));
    }
    if (path.endsWith("/axis-cgi/applications/list.cgi")) {
      return Promise.resolve(response(sample.apps || "", "application/xml; charset=utf-8"));
    }
    if (path.endsWith("/axis-cgi/param.cgi")) {
      if (method.toUpperCase() !== "GET") return Promise.resolve(response("OK", "text/plain; charset=utf-8"));
      return Promise.resolve(response(sample.params || "", "text/plain; charset=utf-8"));
    }

    if (app === "wireguard") {
      if (path.endsWith("/api/settings")) return Promise.resolve(json({}));
    }
    if (app === "wireguardServer") {
      if (path.endsWith("/api/settings")) {
        return Promise.resolve(method.toUpperCase() === "GET" ? json(sample.settings) : response("OK", "text/plain; charset=utf-8"));
      }
      if (path.endsWith("/status")) return Promise.resolve(json(sample.status));
    }
    if (app === "zerotier") {
      if (path.endsWith("/status.json")) return Promise.resolve(json(sample.status));
      if (path.endsWith("/api/settings")) {
        return Promise.resolve(method.toUpperCase() === "GET" ? json({}) : response("OK", "text/plain; charset=utf-8"));
      }
    }
    if (app === "netbird") {
      if (path.endsWith("/api/status")) return Promise.resolve(json(sample.status));
      if (path.endsWith("/config/settings")) {
        return Promise.resolve(method.toUpperCase() === "GET" ? json({}) : response("OK", "text/plain; charset=utf-8"));
      }
    }
    if (app === "openvpn") {
      if (path.endsWith("/status.json")) return Promise.resolve(json(sample.status));
      if (path.endsWith("/api/settings")) {
        return Promise.resolve(method.toUpperCase() === "GET" ? json({}) : response("OK", "text/plain; charset=utf-8"));
      }
      if (path.endsWith("/api/profile")) return Promise.resolve(response("OK", "text/plain; charset=utf-8"));
    }
    if (app === "openconnect") {
      if (path.endsWith("/api/status")) return Promise.resolve(json(sample.status));
      if (path.endsWith("/api/settings")) {
        return Promise.resolve(method.toUpperCase() === "GET" ? json(sample.settings) : response('{"changed":false}', "application/json; charset=utf-8"));
      }
      if (/\/api\/(login|answer|cancel|sso|cert|ca|reconnect)$/.test(path)) {
        return Promise.resolve(json({ ok: true, changed: false }));
      }
    }
    if (app === "tailcat") {
      if (path.endsWith("/api/status")) return Promise.resolve(json(sample.status));
      if (path.endsWith("/api/settings")) {
        return Promise.resolve(method.toUpperCase() === "GET" ? json(sample.settings) : json({ ok: true, changed: false }));
      }
      if (/\/api\/(start|stop|forget-key)$/.test(path)) return Promise.resolve(json(sample.status));
    }

    if (path.endsWith("/api/status")) return Promise.resolve(json(sample.status || {}));
    return Promise.resolve(response("No sample endpoint is configured for this request.", "text/plain; charset=utf-8", 404));
  };

  window.open = function () {
    window.__previewBlockedExternal.push("window.open");
    return null;
  };
  document.addEventListener("click", function (event) {
    if (app === "wireguard" && event.target.closest && event.target.closest("#gen-btn")) {
      event.preventDefault();
      event.stopImmediatePropagation();
      const message = document.getElementById("cfg-msg");
      message.textContent = "Key generation is disabled in this sample preview.";
      message.className = "cfg-msg";
      return;
    }
    const link = event.target.closest && event.target.closest("a[href]");
    if (!link) return;
    const target = new URL(link.href, window.location.href);
    if (target.origin !== window.location.origin || target.protocol === "javascript:") {
      event.preventDefault();
      event.stopImmediatePropagation();
      window.__previewBlockedExternal.push(target.href);
    }
  }, true);
  document.addEventListener("submit", function (event) {
    if (app !== "wireguardServer" || event.target.id !== "add-peer") return;
    event.preventDefault();
    event.stopImmediatePropagation();
    document.getElementById("message").textContent = "Peer creation is disabled in this sample preview.";
  }, true);

  window.addEventListener("DOMContentLoaded", function () {
    const notice = document.createElement("aside");
    notice.textContent = "Offline preview · sample data only · settings and service actions do not affect a camera.";
    notice.setAttribute("role", "note");
    notice.style.cssText = "position:sticky;top:0;z-index:9999;margin:0 auto 14px;padding:8px 12px;border:1px solid #d59b36;border-radius:6px;background:#fff6e0;color:#583b00;font:12px/1.4 -apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;text-align:center";
    document.body.prepend(notice);
  });
})();
