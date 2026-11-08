import { csvCell } from './account-tools';
describe('CSV export', () => {
  it('spreadsheet formula prefixes and quotes remain literal', () => {
    expect(csvCell('  =1+1')).toBe('"\'  =1+1"');
    expect(csvCell('a"b')).toBe('"a""b"');
  });
});
