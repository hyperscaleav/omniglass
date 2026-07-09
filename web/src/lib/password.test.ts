import { describe, it, expect } from "vitest";
import { generatePassword } from "./password";
import { passwordError } from "./validate";

describe("generatePassword", () => {
  it("honors the requested length (default 20)", () => {
    expect(generatePassword().length).toBe(20);
    expect(generatePassword(32).length).toBe(32);
  });

  it("omits look-alike characters (0 O 1 l I)", () => {
    const pw = generatePassword(400);
    expect(pw).not.toMatch(/[0O1lI]/);
  });

  it("is different across calls (crypto-random)", () => {
    expect(generatePassword()).not.toBe(generatePassword());
  });

  it("always satisfies the client password policy, including versus a username", () => {
    for (let i = 0; i < 50; i++) {
      expect(passwordError(generatePassword(), "jordan")).toBeNull();
    }
  });
});
