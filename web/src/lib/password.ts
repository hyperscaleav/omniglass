// A crypto-strong random password generator (issue #104), for the New user and
// change-password forms' Generate action. The charset is readable: it omits the
// look-alike characters (0/O, 1/l/I) so an admin can transcribe or dictate the
// password without ambiguity. At the default length it carries well over 100 bits of
// entropy, so a generated password always satisfies the server password policy (far
// past the length floor, never a common password, never containing a username).

// Readable strong charset: lowercase and uppercase without look-alikes, digits 2-9,
// and two separators. ~57 symbols.
const CHARSET = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789-_";

// generatePassword returns a random password of the given length (default 20) drawn
// uniformly from the readable charset using crypto.getRandomValues.
export function generatePassword(length = 20): string {
  const buf = new Uint32Array(length);
  crypto.getRandomValues(buf);
  let out = "";
  for (let i = 0; i < length; i++) out += CHARSET[buf[i] % CHARSET.length];
  return out;
}
