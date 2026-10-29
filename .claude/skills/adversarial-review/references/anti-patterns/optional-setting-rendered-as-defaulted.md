## optional-setting-rendered-as-defaulted (fixed; found building #1350)

- **Cue:** a settings consumer (the console's Settings page, a reader of the effective
  document) that assumes every key resolved a value and that a missing source means
  "declared default", in a diff that adds a setting with no `default` tag.
- **Failure:** an optional key no level set is either never listed (the console offers no way
  to set it), validated as a blank and shown red in edit mode on a row nobody touched, or drawn
  in the layer stack as resolved by a declared default it does not have, teaching the operator
  a wrong fact about the cascade.
- **Fix:** list keys from the generated schema as well as the effective document
  (`settingKeys`, web/src/lib/settingsValidation.ts), give an unresolved optional key a source of
  its own (`UNSET`, web/src/pages/Settings.tsx) that marks no level effective and reads the
  setting's description, and validate its draft only once it is typed in.
