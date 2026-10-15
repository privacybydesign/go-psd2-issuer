import { describe, it, expect, beforeAll } from 'vitest';
import { render, screen } from '@testing-library/react';
import { AttributeTable, isEmpty, orderedAttributes } from './components';
import { Definitions } from './types';
import i18n from './i18n';

const text = (en: string, nl = en) => ({ en, nl });

const definitions: Definitions = {
  period_attributes: [
    { id: 'months', name: text('Months analysed'), description: text('') },
    { id: 'periodstart', name: text('Period from'), description: text('') },
    { id: 'periodend', name: text('Period until'), description: text('') },
  ],
  credentials: [
    {
      key: 'housing',
      e1_group: 'housing',
      name: text('Housing'),
      description: text(''),
      attributes: [
        { id: 'rent', e1: 'housing.rent', name: text('Rent'), description: text('') },
        { id: 'mortgage', e1: 'housing.mortgage', name: text('Mortgage'), description: text('') },
      ],
    },
    {
      key: 'bankaccount',
      name: text('Bank account'),
      description: text(''),
      attributes: [
        { id: 'iban', name: text('IBAN'), description: text('') },
        { id: 'bank', name: text('Bank'), description: text('') },
      ],
    },
  ],
};

describe('AttributeTable', () => {
  beforeAll(async () => {
    await i18n.changeLanguage('en');
  });

  it('formats signed amounts, counts and dates', () => {
    render(
      <AttributeTable
        credential={{ key: 'housing', attributes: { rent: '-1050', mortgage: '0', months: '12', periodstart: '2025-10-01' } }}
        definitions={definitions}
      />,
    );
    expect(screen.getByText('Rent')).toBeInTheDocument();
    expect(screen.getByText('-€1,050')).toBeInTheDocument();
    expect(screen.getByText('€0')).toBeInTheDocument();
    expect(screen.getByText('12')).toBeInTheDocument();
    expect(screen.getByText('1 October 2025')).toBeInTheDocument();
  });

  it('does not format IBANs or bank identifiers', () => {
    render(
      <AttributeTable credential={{ key: 'bankaccount', attributes: { iban: 'NL91ABNA0417164300', bank: '123' } }} definitions={definitions} />,
    );
    expect(screen.getByText('NL91ABNA0417164300')).toBeInTheDocument();
    expect(screen.getByText('123')).toBeInTheDocument();
  });
});

describe('orderedAttributes', () => {
  it('orders by the definition with the period last', () => {
    const names = orderedAttributes(
      { key: 'housing', attributes: { periodend: 'x', mortgage: '1', months: '12', rent: '1', zzz: 'unknown' } },
      definitions.credentials[0],
      definitions.period_attributes,
    ).map(([name]) => name);
    expect(names).toEqual(['rent', 'mortgage', 'months', 'periodend', 'zzz']);
  });
});

describe('isEmpty', () => {
  it('is true when every amount is zero', () => {
    const housing = definitions.credentials[0];
    expect(isEmpty({ key: 'housing', attributes: { rent: '0', mortgage: '0', months: '12' } }, housing)).toBe(true);
    expect(isEmpty({ key: 'housing', attributes: { rent: '-10', mortgage: '0' } }, housing)).toBe(false);
    expect(isEmpty({ key: 'bankaccount', attributes: { iban: 'x' } }, definitions.credentials[1])).toBe(false);
  });
});
