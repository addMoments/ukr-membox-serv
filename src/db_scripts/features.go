package dbscripts

const (
	// FeatureQR, etkinlikte QR ozelliklerinin kullanilabildigini belirtir.
	FeatureQR     = 1
	// FeaturePoster, etkinlikte poster/welcome board benzeri gorsel ozelliklerin acik oldugunu belirtir.
	FeaturePoster = 2
	// FeatureVoice, sesli mesaj/voice add-on hakkinin acik oldugunu belirtir.
	FeatureVoice  = 3
	// FeatureAdvertorial, guest sayfasindaki reklam alani (layout + banner hucreleri) hakkini belirtir.
	FeatureAdvertorial = 4
	// FeatureAddmomentsAds, reklam alanini AddMoments'in yonettigini belirtir (Excel madde 6 ozel paketleri):
	// misafir sayfasi site_settings.addmoments_ads reklamini gosterir, host alani duzenleyemez. 4'ten onceliklidir.
	FeatureAddmomentsAds = 5
)
