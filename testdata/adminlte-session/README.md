# AdminLTE + session-auth test application

This is the active integration fixture for spec 081. It deliberately starts
small: an AdminLTE-shaped dashboard is authored as `*.blade.php`, compiled to a
typed Go renderer, and protected by Pickle's session auth driver.

The CSS in this first slice is a tiny Pickle-owned compatibility fixture using
AdminLTE's public class vocabulary. It is not the upstream AdminLTE
distribution. The pinned upstream assets, license/provenance manifest,
content-addressed embedding, and scaffold installer land in subsequent 081
slices.
