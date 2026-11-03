import Foundation

// Paywall wording; prices are always the App Store's own display strings, never computed here.
enum PlanText {
    static func title(_ term: PlanTerm) -> String {
        switch term {
        case .month: return String(localized: "paywall_plan_month", defaultValue: "1 month")
        case .quarter: return String(localized: "paywall_plan_quarter", defaultValue: "3 months")
        case .year: return String(localized: "paywall_plan_year", defaultValue: "12 months")
        }
    }

    static func price(_ plan: StorePlan) -> String {
        let format: String
        switch plan.term {
        case .month: format = String(localized: "paywall_price_month", defaultValue: "%@ per month")
        case .quarter: format = String(localized: "paywall_price_quarter", defaultValue: "%@ every 3 months")
        case .year: format = String(localized: "paywall_price_year", defaultValue: "%@ per year")
        }
        return String(format: format, plan.displayPrice)
    }

    static func action(trial: Bool) -> String {
        trial
            ? String(localized: "paywall_start_trial", defaultValue: "Start free week")
            : String(localized: "paywall_subscribe", defaultValue: "Subscribe")
    }

    static var trialBadge: String {
        String(localized: "paywall_trial_badge", defaultValue: "1 week free")
    }

    // The auto-renewal terms Apple requires beside the buy button, for the plan that button buys.
    static func terms(_ plan: StorePlan?) -> String {
        let renewal = String(localized: "paywall_terms_renewal", defaultValue: "Renews automatically until cancelled. Cancel at least 24 hours before the current period ends in your Apple ID settings.")
        guard let plan else { return renewal }
        let billing = plan.trialEligible
            ? String(localized: "paywall_terms_trial", defaultValue: "Free for 1 week, then %@, billed to your Apple ID. The free week turns into a paid subscription unless you cancel at least 24 hours before it ends.")
            : String(localized: "paywall_terms_paid", defaultValue: "%@, billed to your Apple ID.")
        return String(format: billing, price(plan)) + " " + renewal
    }

    // The sign-in screen's way in; the free week is promised only once the App Store confirms it.
    static func newUserLink(trialEligible: Bool?) -> String {
        trialEligible == true
            ? String(localized: "login_new_free_week", defaultValue: "New to Pangea? Start your free week")
            : String(localized: "login_new_see_plans", defaultValue: "New to Pangea? See plans")
    }
}
