// A Go duration as an operator writes it: 5m0s reads 5m, 1h0m0s reads 1h.
export const every = (d: string) => d.replace(/(\D)0s$/, '$1').replace(/(\D)0m$/, '$1');
