import Foundation

struct Server: Codable, Equatable, Sendable {
    let id: String
    let name: String
    let region: String
    let country: String
    let load: Int?
    // Transports this server offers that this build can run; Cloak and WireGuard always.
    let transports: Set<String>
    // The server may relay as the entry of a two-hop route.
    let multihop: Bool

    static let universalTransports: Set<String> = ["cloak", "wireguard"]

    private enum CodingKeys: String, CodingKey {
        case id, name, region, country, load, multihop
        case reality, hysteria2, shadowsocks, anytls, naive, snowflake
    }

    private static let advertised: [CodingKeys] = [.reality, .hysteria2, .shadowsocks, .anytls, .naive, .snowflake]

    init(id: String, name: String, region: String, country: String, load: Int?,
         transports: Set<String> = Server.universalTransports, multihop: Bool = false) {
        self.id = id
        self.name = name
        self.region = region
        self.country = country
        self.load = load
        self.transports = transports
        self.multihop = multihop
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        name = try c.decode(String.self, forKey: .name)
        region = try c.decode(String.self, forKey: .region)
        country = try c.decode(String.self, forKey: .country)
        load = try c.decodeIfPresent(Int.self, forKey: .load)
        multihop = (try? c.decodeIfPresent(Bool.self, forKey: .multihop)) ?? false
        let present = Self.advertised.filter { Self.offers(c, $0) }
        transports = Self.universalTransports.union(present.map(\.rawValue))
    }

    // The core sends a flag per transport; a raw hub block (an object) also counts as offered.
    private static func offers(_ c: KeyedDecodingContainer<CodingKeys>, _ key: CodingKeys) -> Bool {
        if let flag = try? c.decodeIfPresent(Bool.self, forKey: key) { return flag }
        return (try? c.decodeNil(forKey: key)) == false
    }

    func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(id, forKey: .id)
        try c.encode(name, forKey: .name)
        try c.encode(region, forKey: .region)
        try c.encode(country, forKey: .country)
        try c.encodeIfPresent(load, forKey: .load)
        try c.encode(multihop, forKey: .multihop)
        for key in Self.advertised { try c.encode(transports.contains(key.rawValue), forKey: key) }
    }

    func supports(_ transport: String) -> Bool {
        transport == "auto" || transports.contains(transport)
    }
}

struct Session: Codable, Equatable, Sendable {
    let email: String
    let name: String
    let servers: [Server]
}

struct TunnelConfig: Codable, Equatable, Sendable {
    let address: String
    let prefixLength: Int
    let dns: [String]
    let mtu: Int
    let serverId: String
    let serverName: String
}

struct Subscription: Codable, Equatable, Sendable {
    let status: String
    let renews: Bool
    let expiresAt: String?
    // nil when the hub did not say; only an explicit false blocks connecting.
    let entitled: Bool?
    // Served from the last answer because the hub was unreachable.
    let cached: Bool

    init(status: String, renews: Bool, expiresAt: String?, entitled: Bool? = nil, cached: Bool = false) {
        self.status = status
        self.renews = renews
        self.expiresAt = expiresAt
        self.entitled = entitled
        self.cached = cached
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        status = try c.decode(String.self, forKey: .status)
        renews = try c.decodeIfPresent(Bool.self, forKey: .renews) ?? false
        expiresAt = try c.decodeIfPresent(String.self, forKey: .expiresAt)
        entitled = try c.decodeIfPresent(Bool.self, forKey: .entitled)
        cached = try c.decodeIfPresent(Bool.self, forKey: .cached) ?? false
    }
}

struct Device: Codable, Equatable, Sendable {
    let id: String
    let friendlyName: String?
    let createdAt: String
    let status: String
    let current: Bool

    init(id: String, friendlyName: String?, createdAt: String, status: String, current: Bool = false) {
        self.id = id
        self.friendlyName = friendlyName
        self.createdAt = createdAt
        self.status = status
        self.current = current
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        friendlyName = try c.decodeIfPresent(String.self, forKey: .friendlyName)
        createdAt = try c.decodeIfPresent(String.self, forKey: .createdAt) ?? ""
        status = try c.decodeIfPresent(String.self, forKey: .status) ?? ""
        current = try c.decodeIfPresent(Bool.self, forKey: .current) ?? false
    }
}

struct HubTestResult: Codable, Equatable, Sendable {
    let method: String
    let ok: Bool
    let ms: Int
    let detail: String
    // Why a method could not be tried at all: noAddress, noCredentials, noRelay or busy.
    let unavailable: String

    init(method: String = "", ok: Bool, ms: Int, detail: String = "", unavailable: String = "") {
        self.method = method
        self.ok = ok
        self.ms = ms
        self.detail = detail
        self.unavailable = unavailable
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        method = try c.decodeIfPresent(String.self, forKey: .method) ?? ""
        ok = try c.decodeIfPresent(Bool.self, forKey: .ok) ?? false
        ms = try c.decodeIfPresent(Int.self, forKey: .ms) ?? 0
        detail = try c.decodeIfPresent(String.self, forKey: .detail) ?? ""
        unavailable = try c.decodeIfPresent(String.self, forKey: .unavailable) ?? ""
    }
}

struct HubStatus: Codable, Equatable, Sendable {
    // The method this process last reached the hub by; nil before the first request.
    let active: String?
    let detail: String?

    static let none = HubStatus(active: nil, detail: nil)
}

enum TunnelState: String, Codable, Equatable, Sendable {
    case disconnected = "DISCONNECTED"
    case connecting = "CONNECTING"
    case connected = "CONNECTED"
    case disconnecting = "DISCONNECTING"
    case error = "ERROR"

    init(from decoder: Decoder) throws {
        let raw = try decoder.singleValueContainer().decode(String.self)
        self = TunnelState(rawValue: raw) ?? .error
    }
}

struct TunnelStatus: Codable, Equatable, Sendable {
    var state: TunnelState
    var detail: String
    var bytesIn: Int64
    var bytesOut: Int64
    var serverId: String
    var serverName: String
    var activeTransport: String?
    // Unix seconds of the newest WireGuard handshake, 0 before the first one.
    var lastHandshake: Int64
    var postQuantum: Bool
    // Empty unless the route is multihop; serverId stays the exit.
    var entryServerId: String
    var entryServerName: String

    static let disconnected = TunnelStatus(
        state: .disconnected, detail: "", bytesIn: 0, bytesOut: 0,
        serverId: "", serverName: "", activeTransport: nil)

    init(state: TunnelState, detail: String, bytesIn: Int64, bytesOut: Int64,
         serverId: String, serverName: String, activeTransport: String?,
         lastHandshake: Int64 = 0, postQuantum: Bool = false,
         entryServerId: String = "", entryServerName: String = "") {
        self.state = state
        self.detail = detail
        self.bytesIn = bytesIn
        self.bytesOut = bytesOut
        self.serverId = serverId
        self.serverName = serverName
        self.activeTransport = activeTransport
        self.lastHandshake = lastHandshake
        self.postQuantum = postQuantum
        self.entryServerId = entryServerId
        self.entryServerName = entryServerName
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        state = try c.decodeIfPresent(TunnelState.self, forKey: .state) ?? .error
        detail = try c.decodeIfPresent(String.self, forKey: .detail) ?? ""
        bytesIn = try c.decodeIfPresent(Int64.self, forKey: .bytesIn) ?? 0
        bytesOut = try c.decodeIfPresent(Int64.self, forKey: .bytesOut) ?? 0
        serverId = try c.decodeIfPresent(String.self, forKey: .serverId) ?? ""
        serverName = try c.decodeIfPresent(String.self, forKey: .serverName) ?? ""
        activeTransport = try c.decodeIfPresent(String.self, forKey: .activeTransport)
        lastHandshake = try c.decodeIfPresent(Int64.self, forKey: .lastHandshake) ?? 0
        postQuantum = try c.decodeIfPresent(Bool.self, forKey: .postQuantum) ?? false
        entryServerId = try c.decodeIfPresent(String.self, forKey: .entryServerId) ?? ""
        entryServerName = try c.decodeIfPresent(String.self, forKey: .entryServerName) ?? ""
    }
}

enum Transport: String, CaseIterable, Sendable {
    case auto, cloak, reality, shadowsocks, hysteria2, anytls
}

struct HubMethods: Codable, Equatable, Sendable {
    var directIp = true
    var reality = true
    var shadowsocks = true
    var fronted = true
    var normal = false
    var rev = 2

    init() {}

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        let d = HubMethods()
        directIp = try c.decodeIfPresent(Bool.self, forKey: .directIp) ?? d.directIp
        reality = try c.decodeIfPresent(Bool.self, forKey: .reality) ?? d.reality
        shadowsocks = try c.decodeIfPresent(Bool.self, forKey: .shadowsocks) ?? d.shadowsocks
        fronted = try c.decodeIfPresent(Bool.self, forKey: .fronted) ?? d.fronted
        normal = try c.decodeIfPresent(Bool.self, forKey: .normal) ?? d.normal
        rev = try c.decodeIfPresent(Int.self, forKey: .rev) ?? d.rev
    }
}

struct AppSettings: Codable, Equatable, Sendable {
    var preferredTransport = Transport.auto.rawValue
    var customDns: [String] = []
    var mtu = 1380
    var allowLan = false
    var autoConnect = false
    var lastServerId = ""
    var hubMethods = HubMethods()
    var deadDrop = true
    var postQuantum = false

    init() {}

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        let d = AppSettings()
        preferredTransport = try c.decodeIfPresent(String.self, forKey: .preferredTransport) ?? d.preferredTransport
        customDns = try c.decodeIfPresent([String].self, forKey: .customDns) ?? d.customDns
        mtu = try c.decodeIfPresent(Int.self, forKey: .mtu) ?? d.mtu
        allowLan = try c.decodeIfPresent(Bool.self, forKey: .allowLan) ?? d.allowLan
        autoConnect = try c.decodeIfPresent(Bool.self, forKey: .autoConnect) ?? d.autoConnect
        lastServerId = try c.decodeIfPresent(String.self, forKey: .lastServerId) ?? d.lastServerId
        hubMethods = try c.decodeIfPresent(HubMethods.self, forKey: .hubMethods) ?? d.hubMethods
        deadDrop = try c.decodeIfPresent(Bool.self, forKey: .deadDrop) ?? d.deadDrop
        postQuantum = try c.decodeIfPresent(Bool.self, forKey: .postQuantum) ?? d.postQuantum
    }
}

extension Session {
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        email = try c.decode(String.self, forKey: .email)
        name = try c.decode(String.self, forKey: .name)
        servers = try c.decodeIfPresent([Server].self, forKey: .servers) ?? []
    }
}

extension TunnelConfig {
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        address = try c.decode(String.self, forKey: .address)
        prefixLength = try c.decode(Int.self, forKey: .prefixLength)
        dns = try c.decodeIfPresent([String].self, forKey: .dns) ?? []
        mtu = try c.decode(Int.self, forKey: .mtu)
        serverId = try c.decode(String.self, forKey: .serverId)
        serverName = try c.decode(String.self, forKey: .serverName)
    }
}
