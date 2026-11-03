import XCTest
@testable import PangeaVPN

@MainActor
final class TransportLabelsTests: XCTestCase {
    func testPickerListsEveryTransportOnceInDesktopOrder() {
        let order = TransportLabels.pickerOrder
        XCTAssertEqual(order, [.auto, .reality, .cloak, .shadowsocks, .hysteria2, .anytls])
        XCTAssertEqual(order.count, Transport.allCases.count)
        XCTAssertEqual(Set(order), Set(Transport.allCases))
    }

    func testEveryTransportHasItsOwnDescription() {
        let descriptions = Transport.allCases.map { TransportLabels.description($0) }
        XCTAssertFalse(descriptions.contains(where: \.isEmpty))
        XCTAssertEqual(Set(descriptions).count, Transport.allCases.count)
    }

    func testDescriptionsMatchDesktop() {
        XCTAssertEqual(TransportLabels.description(.auto), "Tries each disguise in turn and remembers what worked on this network.")
        XCTAssertEqual(TransportLabels.description(.reality), "Borrows a real site's TLS handshake, so probes see a genuine site. The strongest disguise.")
        XCTAssertEqual(TransportLabels.description(.shadowsocks), "No TLS at all, so a block aimed at TLS misses it.")
        XCTAssertEqual(TransportLabels.description(.hysteria2), "UDP and QUIC instead of TCP. Worth a try when every TCP connection dies.")
    }

    func testSelectedReadsTheStoredChoice() {
        XCTAssertEqual(TransportLabels.selected("reality"), .reality)
        XCTAssertEqual(TransportLabels.selected("anytls"), .anytls)
        XCTAssertEqual(TransportLabels.selected("auto"), .auto)
    }

    func testSelectedFallsBackToAutomaticForChoicesWithoutARow() {
        XCTAssertEqual(TransportLabels.selected("naive"), .auto)
        XCTAssertEqual(TransportLabels.selected("wireguard"), .auto)
        XCTAssertEqual(TransportLabels.selected(""), .auto)
    }

    func testStatusAndChoiceKeepWorking() {
        XCTAssertEqual(TransportLabels.status("anytls"), "AnyTLS")
        XCTAssertEqual(TransportLabels.status(nil), "")
        XCTAssertEqual(TransportLabels.choice(.auto), "Automatic (recommended)")
        XCTAssertEqual(TransportLabels.choice(.reality), "VLESS+REALITY")
    }

    func testSummaryIsTheSelectedTitle() {
        var settings = AppSettings()
        XCTAssertEqual(SettingsSummary.transport(settings), "Automatic (recommended)")
        settings.preferredTransport = "hysteria2"
        XCTAssertEqual(SettingsSummary.transport(settings), "Hysteria2")
        settings.preferredTransport = "wireguard"
        XCTAssertEqual(SettingsSummary.transport(settings), "Automatic (recommended)")
    }
}
