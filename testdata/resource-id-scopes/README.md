# ResourceID scope fixture

This source fixture models records whose database identity is
`(organization_id, record_id)`. The public `ResourceID` encodes those two
integers, but the controller still compares the decoded scope with trusted
request authority and queries with both columns.

`UnsafeShow` is intentionally incorrect and exists as a positive Squeeze
fixture: it loads by `RecordID` alone. `Show` is the accepted form.
