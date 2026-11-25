/** One function per value JSON cannot carry: a bridge that drops any of them says "mismatch" here. */

export const half = (n: number): number => n / 2;
export const divide = (a: number, b: number): number => a / b;
export const negativeZero = (): number => -0;
export const keep = (v: unknown): unknown => v;
export const loneSurrogate = (): string => "\ud800";
export const undefinedValue = (): unknown => undefined;
export const isNegativeZero = (n: number): boolean => Object.is(n, -0);
