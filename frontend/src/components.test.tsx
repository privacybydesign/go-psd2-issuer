import { describe, it, expect, beforeAll } from 'vitest';
import { render, screen } from '@testing-library/react';
import { AttributeTable, orderedAttributes } from './components';
import i18n from './i18n';

describe('AttributeTable', () => {
  beforeAll(async () => {
    await i18n.changeLanguage('en');
  });

  it('formats amounts, enums, counts and dates', () => {
    render(
      <AttributeTable
        credential={{
          key: 'housing',
          attributes: { housingtype: 'rent', housingcost: '1050', months: '12', periodstart: '2025-10-01' },
        }}
      />,
    );
    expect(screen.getByText('Housing')).toBeInTheDocument();
    expect(screen.getByText('Rent')).toBeInTheDocument();
    expect(screen.getByText('€1,050')).toBeInTheDocument();
    expect(screen.getByText('12')).toBeInTheDocument();
    expect(screen.getByText('1 October 2025')).toBeInTheDocument();
  });

  it('does not format IBANs or names', () => {
    render(
      <AttributeTable
        credential={{ key: 'bankaccount', attributes: { iban: 'NL91ABNA0417164300', accountholder: 'J. Jansen', bank: 'other' } }}
      />,
    );
    expect(screen.getByText('NL91ABNA0417164300')).toBeInTheDocument();
    expect(screen.getByText('other')).toBeInTheDocument();
  });
});

describe('orderedAttributes', () => {
  it('orders by the defined order with the period last', () => {
    const names = orderedAttributes({
      key: 'income',
      attributes: { periodend: 'x', total: '1', months: '12', salary: '1', zzz: 'unknown' },
    }).map(([name]) => name);
    expect(names).toEqual(['salary', 'total', 'months', 'periodend', 'zzz']);
  });
});
