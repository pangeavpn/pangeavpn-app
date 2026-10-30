import XCTest
@testable import PangeaVPN

final class LanguageNamesTests: XCTestCase {
    func testSupportedListMatchesDesktopInOrder() {
        XCTAssertEqual(LanguageNames.supported.map { $0.name }, [
            "English", "Español", "Français", "Русский", "Українська", "中文", "العربية", "فارسی",
        ])
        XCTAssertEqual(LanguageNames.supported.map { $0.code }, ["en", "es", "fr", "ru", "uk", "zh", "ar", "fa"])
    }

    func testEveryShippedLocalizationHasANativeName() {
        let shipped = ["en", "es", "fr", "ru", "uk", "zh-Hans", "ar", "fa"]
        let names = shipped.map { LanguageNames.nativeName(for: $0) }
        XCTAssertEqual(names, LanguageNames.supported.map { $0.name })
    }

    func testRegionAndScriptVariantsUseTheLanguage() {
        XCTAssertEqual(LanguageNames.nativeName(for: "zh-Hans"), "中文")
        XCTAssertEqual(LanguageNames.nativeName(for: "zh_Hant_TW"), "中文")
        XCTAssertEqual(LanguageNames.nativeName(for: "en-GB"), "English")
        XCTAssertEqual(LanguageNames.nativeName(for: "pt_BR"), "English")
        XCTAssertEqual(LanguageNames.nativeName(for: "FR"), "Français")
        XCTAssertEqual(LanguageNames.languageCode("uk-UA"), "uk")
    }

    func testUnknownOrMissingReadsAsEnglish() {
        XCTAssertEqual(LanguageNames.nativeName(for: nil), "English")
        XCTAssertEqual(LanguageNames.nativeName(for: "Base"), "English")
        XCTAssertEqual(LanguageNames.nativeName(for: ""), "English")
    }

    @MainActor
    func testCurrentLanguageIsOneWeShip() {
        XCTAssertTrue(LanguageNames.supported.contains(LanguageNames.current))
        XCTAssertEqual(SettingsSummary.language(), LanguageNames.current.name)
    }
}
