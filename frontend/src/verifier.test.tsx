import { describe, it, expect, beforeAll } from 'vitest';
import { render, screen } from '@testing-library/react';
import { VerifierResultView, articleUrl } from './pages/Verifier';
import { VerifierResult } from './types';
import i18n from './i18n';

const result: VerifierResult = {
  household: { age_group: 'adult', partner: false, partner_pension_age: false, housemates: 1 },
  disclosed: ['income', 'subsidy'],
  evaluation: {
    source: { law: 'Participatiewet', bwb_id: 'BWBR0015703', version: '2026-04-03', url: 'https://wetten.overheid.nl/BWBR0015703/2026-04-03' },
    counted: [{ attribute: 'income.salary', amount: 80000, article: '32.1.a' }],
    excluded: [{ attribute: 'subsidy.rent', amount: 30000, article: '31.2.d', reason: 'awir' }],
    income: 80000,
    results: [
      {
        rule: 'bijstand',
        outcome: 'indication',
        steps: [
          { key: 'cost_sharers', article: '22a.1', amount: 2 },
          { key: 'norm_cost_sharing', article: '22a.1.a', amount: 100107 },
          { key: 'income', article: '32', amount: 80000 },
          { key: 'supplement', article: '19.1', amount: 20107 },
        ],
        notes: ['note_assets'],
      },
      { rule: 'minima', outcome: 'no_indication' },
    ],
  },
};

describe('VerifierResultView', () => {
  beforeAll(async () => {
    await i18n.changeLanguage('en');
  });

  it('shows the outcome, the calculation and what is not counted', () => {
    render(<VerifierResultView result={result} />);
    expect(screen.getByText('Social assistance (bijstand)')).toBeInTheDocument();
    expect(screen.getByText(/You may be entitled to a supplement/)).toBeInTheDocument();
    expect(screen.getByText('Your income is above the example income limit.')).toBeInTheDocument();
    // a count, not an amount
    expect(screen.getByText('2')).toBeInTheDocument();
    expect(screen.getByText('€1,001.07')).toBeInTheDocument();
    expect(screen.getByText('€201.07')).toBeInTheDocument();
    expect(screen.getByText('Income per month: €800.00')).toBeInTheDocument();
    expect(screen.getByText(/allowance, does not count/)).toBeInTheDocument();
    expect(screen.getByText('art. 22a.1.a').closest('a')).toHaveAttribute('href', 'https://wetten.overheid.nl/BWBR0015703/2026-04-03#Artikel22a');
  });
});

describe('articleUrl', () => {
  it('links to the article of the version used', () => {
    expect(articleUrl(result.evaluation, '31.2.d')).toBe('https://wetten.overheid.nl/BWBR0015703/2026-04-03#Artikel31');
  });
});
