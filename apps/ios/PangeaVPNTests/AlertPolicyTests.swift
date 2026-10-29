import XCTest
@testable import PangeaVPN

final class AlertPolicyTests: XCTestCase {
    private let background = AlertPolicy.Gate(enabled: true, appInForeground: false)
    private let foreground = AlertPolicy.Gate(enabled: true, appInForeground: true)
    private let switchedOff = AlertPolicy.Gate(enabled: false, appInForeground: false)
    private let outage: [ConnectionAlert.Kind] = [.lost, .blocked]

    private func policyShowingLost() -> AlertPolicy {
        var policy = AlertPolicy()
        _ = policy.decide(.reconnecting, gate: background)
        _ = policy.lostDelayElapsed(gate: background)
        return policy
    }

    func testRecoveringSchedulesLostAfterTenSeconds() {
        var policy = AlertPolicy()

        XCTAssertEqual(policy.decide(.reconnecting, gate: background), [.scheduleLost(after: 10)])
        XCTAssertEqual(policy.lostDelayElapsed(gate: background), [.post(.lost)])
    }

    func testQuickBlipOnlyCancelsTheTimer() {
        var policy = AlertPolicy()
        _ = policy.decide(.reconnecting, gate: background)

        XCTAssertEqual(policy.decide(.restored, gate: background), [.cancelLost])
        XCTAssertEqual(policy.lostDelayElapsed(gate: background), [])
    }

    func testRepeatedRecoveringKeepsTheFirstTimer() {
        var policy = AlertPolicy()
        _ = policy.decide(.reconnecting, gate: background)

        XCTAssertEqual(policy.decide(.reconnecting, gate: background), [])
    }

    func testBackOnlineFollowsAShownLostAndClearsIt() {
        var policy = policyShowingLost()

        XCTAssertEqual(policy.decide(.restored, gate: background), [.remove(outage), .post(.restored)])
        XCTAssertEqual(policy.decide(.reconnecting, gate: background), [.scheduleLost(after: 10)])
    }

    func testLostIsNotShownWhileTheAppIsOpen() {
        var policy = AlertPolicy()
        _ = policy.decide(.reconnecting, gate: background)

        XCTAssertEqual(policy.lostDelayElapsed(gate: foreground), [])
        XCTAssertEqual(policy.decide(.restored, gate: background), [])
    }

    func testNothingIsShownWhenAlertsAreOff() {
        var policy = AlertPolicy()
        _ = policy.decide(.reconnecting, gate: switchedOff)

        XCTAssertEqual(policy.lostDelayElapsed(gate: switchedOff), [])
        XCTAssertEqual(policy.decide(.blocked(.signedOut), gate: switchedOff), [])
        XCTAssertEqual(policy.decide(.connected, gate: switchedOff), [.remove(outage)])
    }

    func testRestoreWithTheAppOpenStillClearsAShownLost() {
        var policy = policyShowingLost()

        XCTAssertEqual(policy.decide(.restored, gate: foreground), [.remove(outage)])
    }

    func testBlockedPostsOncePerOutageWithItsReason() {
        var policy = AlertPolicy()

        XCTAssertEqual(
            policy.decide(.blocked(.subscriptionExpired), gate: background),
            [.remove([.lost]), .post(.blocked(.subscriptionExpired))])
        XCTAssertEqual(policy.decide(.blocked(.signedOut), gate: background), [])
        XCTAssertEqual(policy.decide(.reconnecting, gate: background), [])
        XCTAssertEqual(policy.decide(.restored, gate: background), [.remove(outage), .post(.restored)])
        XCTAssertEqual(
            policy.decide(.blocked(.signedOut), gate: background),
            [.remove([.lost]), .post(.blocked(.signedOut))])
    }

    func testBlockedReplacesAPendingLost() {
        var policy = AlertPolicy()
        _ = policy.decide(.reconnecting, gate: background)

        XCTAssertEqual(
            policy.decide(.blocked(.noSubscription), gate: background),
            [.cancelLost, .remove([.lost]), .post(.blocked(.noSubscription))])
        XCTAssertEqual(policy.lostDelayElapsed(gate: background), [])
    }

    func testBlockedFollowsAShownLost() {
        var policy = policyShowingLost()

        XCTAssertEqual(
            policy.decide(.blocked(.signedOut), gate: background),
            [.remove([.lost]), .post(.blocked(.signedOut))])
    }

    func testBlockedHiddenByTheOpenAppCanShowOnceItCloses() {
        var policy = AlertPolicy()

        XCTAssertEqual(policy.decide(.blocked(.signedOut), gate: foreground), [])
        XCTAssertEqual(
            policy.decide(.blocked(.signedOut), gate: background),
            [.remove([.lost]), .post(.blocked(.signedOut))])
    }

    func testBackgroundStartSaysConnected() {
        var policy = AlertPolicy()

        XCTAssertEqual(policy.decide(.connected, gate: background), [.remove(outage), .post(.connected)])
    }

    func testStartFromTheOpenAppOnlyClearsStaleAlerts() {
        var policy = AlertPolicy()

        XCTAssertEqual(policy.decide(.connected, gate: foreground), [.remove(outage)])
    }

    func testStartAfterAnAnnouncedOutageSaysBackOnline() {
        var policy = AlertPolicy()
        _ = policy.decide(.blocked(.signedOut), gate: background)

        XCTAssertEqual(policy.decide(.connected, gate: background), [.remove(outage), .post(.restored)])
    }

    func testStartCancelsAPendingLost() {
        var policy = AlertPolicy()
        _ = policy.decide(.reconnecting, gate: background)

        XCTAssertEqual(policy.decide(.connected, gate: background), [.cancelLost, .remove(outage), .post(.connected)])
    }

    func testStopPostsNothingAndEndsTheOutage() {
        var policy = AlertPolicy()
        _ = policy.decide(.reconnecting, gate: background)

        XCTAssertEqual(policy.decide(.stopped, gate: background), [.cancelLost, .remove(outage)])
        XCTAssertEqual(policy.lostDelayElapsed(gate: background), [])

        var shown = policyShowingLost()
        XCTAssertEqual(shown.decide(.stopped, gate: background), [.remove(outage)])
        XCTAssertEqual(shown.decide(.restored, gate: background), [])
    }

    func testEachKindHasItsOwnIdentifier() {
        let alerts: [ConnectionAlert] = [.connected, .lost, .restored, .blocked(.signedOut)]

        XCTAssertEqual(alerts.map(\.kind), ConnectionAlert.Kind.allCases)
        XCTAssertEqual(Set(ConnectionAlert.Kind.allCases.map(\.identifier)).count, ConnectionAlert.Kind.allCases.count)
    }

    func testOnlyBlockedPlaysASound() {
        let alerts: [ConnectionAlert] = [.connected, .lost, .restored, .blocked(.signedOut)]

        XCTAssertEqual(alerts.map(\.content.playsSound), [false, false, false, true])
    }

    func testBlockedBodyCarriesTheReason() throws {
        let reason = try XCTUnwrap(TunnelStartError.subscriptionExpired.errorDescription)
        let content = ConnectionAlert.blocked(.subscriptionExpired).content

        XCTAssertTrue(content.body.hasPrefix(reason))
        XCTAssertFalse(content.body.contains("%@"))
        XCTAssertFalse(content.title.isEmpty)
    }
}
