import Foundation

struct SubscriptionLine: Equatable, Sendable {
    let text: String
    // Lapsed or unpaid; drawn in the warning colour.
    let warn: Bool
}

// Desktop's account-bar wording; read-only, since iOS shows no purchase or billing link.
enum SubscriptionText {
    static func line(_ sub: Subscription?, locale: Locale = .current, timeZone: TimeZone = .current) -> SubscriptionLine {
        guard let sub else { return SubscriptionLine(text: none, warn: false) }
        let when = HubDate.medium(sub.expiresAt, locale: locale, timeZone: timeZone)
        let base = baseLine(sub, when: when)
        guard sub.cached else { return base }
        let offline = String(localized: "settings_sub_offline", defaultValue: "(offline)")
        return SubscriptionLine(text: "\(base.text) \(offline)", warn: base.warn)
    }

    // Entitlement first: a lapsed prepaid plan still reads "active" and would quote a past date.
    private static func baseLine(_ sub: Subscription, when: String?) -> SubscriptionLine {
        if sub.entitled == false {
            let expired = String(localized: "settings_sub_expired", defaultValue: "Subscription expired")
            return SubscriptionLine(text: dashed(expired, when), warn: true)
        }
        if sub.status == "active" || sub.status == "trialing" {
            let trial = sub.status == "trialing"
                ? String(localized: "settings_sub_trial_prefix", defaultValue: "Free trial · ")
                : ""
            let verb = sub.renews
                ? String(localized: "settings_sub_renews", defaultValue: "Renews")
                : String(localized: "settings_sub_expires", defaultValue: "Expires")
            let dated = when.map { "\(verb) \($0)" } ?? verb
            return SubscriptionLine(text: trial + dated, warn: false)
        }
        if sub.status == "past_due" {
            let pastDue = String(localized: "settings_sub_past_due", defaultValue: "Payment past due")
            return SubscriptionLine(text: dashed(pastDue, when), warn: true)
        }
        return SubscriptionLine(text: none, warn: false)
    }

    private static var none: String {
        String(localized: "subscription_none", defaultValue: "No active subscription")
    }

    private static func dashed(_ text: String, _ when: String?) -> String {
        when.map { "\(text) \u{2014} \($0)" } ?? text
    }
}

// Hub timestamps: RFC 3339 with or without fractions, or a bare calendar date.
enum HubDate {
    static func parse(_ raw: String, timeZone: TimeZone = .current) -> Date? {
        let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return nil }
        let stamped: [ISO8601DateFormatter.Options] = [
            [.withInternetDateTime, .withFractionalSeconds],
            [.withInternetDateTime],
        ]
        for options in stamped {
            if let date = formatter(options, timeZone: TimeZone(identifier: "UTC") ?? timeZone).date(from: trimmed) {
                return date
            }
        }
        // A bare date means that day where the reader is, not midnight UTC.
        return formatter([.withFullDate], timeZone: timeZone).date(from: trimmed)
    }

    // Desktop's "Dec 26, 2026" style, in the reader's locale; nil when absent or unreadable.
    static func medium(_ raw: String?, locale: Locale = .current, timeZone: TimeZone = .current) -> String? {
        guard let raw, let date = parse(raw, timeZone: timeZone) else { return nil }
        let out = DateFormatter()
        out.locale = locale
        out.timeZone = timeZone
        out.dateStyle = .medium
        out.timeStyle = .none
        return out.string(from: date)
    }

    private static func formatter(_ options: ISO8601DateFormatter.Options, timeZone: TimeZone) -> ISO8601DateFormatter {
        let f = ISO8601DateFormatter()
        f.formatOptions = options
        f.timeZone = timeZone
        return f
    }
}
