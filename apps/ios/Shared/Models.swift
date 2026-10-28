import Foundation

struct Server: Codable, Equatable, Sendable {
    let id: String
    let name: String
    let region: String
    let country: String
    let load: Int?
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
}

struct Device: Codable, Equatable, Sendable {
    let id: String
    let friendlyName: String?
    let createdAt: String
    let status: String
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

    static let disconnected = TunnelStatus(
        state: .disconnected, detail: "", bytesIn: 0, bytesOut: 0,
        serverId: "", serverName: "", activeTransport: nil)

    init(state: TunnelState, detail: String, bytesIn: Int64, bytesOut: Int64,
         serverId: String, serverName: String, activeTransport: String?) {
        self.state = state
        self.detail = detail
        self.bytesIn = bytesIn
        self.bytesOut = bytesOut
        self.serverId = serverId
        self.serverName = serverName
        self.activeTransport = activeTransport
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
