import { describe, it, expect, beforeAll } from 'vitest';
import { render, screen } from '@testing-library/react';
import { VerifierResultView, articleUrl } from './pages/Verifier';
import { Source, VerifierResult } from './types';
import i18n from './i18n';

const participatiewet: Source = { id: 'participatiewet', name: 'Participatiewet', version: '2026-07-01', url: 'https://wetten.overheid.nl/BWBR0015703/2026-07-01' };
const uri: Source = { id: 'uitvoeringsregeling_invorderingswet_1990', name: 'Uitvoeringsregeling Invorderingswet 1990', version: '2026-01-01', url: 'https://wetten.overheid.nl/BWBR0004766/2026-01-01' };
const hhnk: Source = { id: 'kwijtscheldingsregeling_hhnk', name: 'Kwijtscheldingsregeling waterschapsbelastingen HHNK 2023', version: '2023-01-01', url: 'https://lokaleregelgeving.overheid.nl/CVDR686061_1' };

const bijstand: VerifierResult = {
  household: { partner: false, partner_pension_age: false, housemates: 1 },
  age_group: 'adult',
  evaluation: {
    law: 'bijstand',
    sources: [participatiewet],
    counted: [{ attribute: 'income.salary', amount: 80000, law: 'participatiewet', article: '32.1.a' }],
    excluded: [{ attribute: 'tax.income', amount: -5000, reason: 'negative' }],
    income: 80000,
    result: {
      rule: 'bijstand',
      outcome: 'indication',
      steps: [
        { key: 'cost_sharers', law: 'participatiewet', article: '22a.1', amount: 2 },
        { key: 'norm_cost_sharing', law: 'participatiewet', article: '22a.1.a', amount: 101390 },
        { key: 'income', law: 'participatiewet', article: '32', amount: 80000 },
        { key: 'supplement', law: 'participatiewet', article: '19.1', amount: 21390 },
      ],
      notes: ['note_assets'],
    },
  },
};

const kwijtschelding: VerifierResult = {
  household: { partner: false, partner_pension_age: false, housemates: 0 },
  age_group: 'adult',
  tax_assessment: 450,
  evaluation: {
    law: 'kwijtschelding_hhnk',
    sources: [uri, participatiewet, hhnk],
    counted: [{ attribute: 'income.salary', amount: 200000, law: uri.id, article: '14.1.a' }],
    excluded: [],
    expenses: [{ attribute: 'housing', amount: 43548, law: uri.id, article: '15.1.b' }],
    income: 200000,
    result: {
      rule: 'kwijtschelding_hhnk',
      outcome: 'partial',
      steps: [
        { key: 'cost_of_living_percentage', law: hhnk.id, article: '2.1', amount: 100 },
        { key: 'payable_per_year', law: uri.id, article: '11.b', amount: 139257 },
      ],
    },
  },
};

describe('VerifierResultView', () => {
  beforeAll(async () => {
    await i18n.changeLanguage('en');
  });

  it('shows the outcome, the calculation and what is not counted', () => {
    render(<VerifierResultView result={bijstand} />);
    expect(screen.getByText('Social assistance (bijstand)')).toBeInTheDocument();
    expect(screen.getByText(/You may be entitled to a supplement/)).toBeInTheDocument();
    expect(screen.getByText(/21 or older, below the state pension age/)).toBeInTheDocument();
    // a count, not an amount
    expect(screen.getByText('2')).toBeInTheDocument();
    expect(screen.getByText('€1,013.90')).toBeInTheDocument();
    expect(screen.getByText('€213.90')).toBeInTheDocument();
    expect(screen.getByText('Income per month: €800.00')).toBeInTheDocument();
    expect(screen.getByText(/more paid back than received/)).toBeInTheDocument();
    expect(screen.getByText('art. 22a.1.a Participatiewet').closest('a')).toHaveAttribute('href', 'https://wetten.overheid.nl/BWBR0015703/2026-07-01#Artikel22a');
  });

  it('shows the expenses, percentages and sources of kwijtschelding', () => {
    render(<VerifierResultView result={kwijtschelding} />);
    expect(screen.getByText('Remission of water board taxes (HHNK)')).toBeInTheDocument();
    expect(screen.getByText(/You pay at most the amount below per year/)).toBeInTheDocument();
    expect(screen.getByText('Housing costs')).toBeInTheDocument();
    expect(screen.getByText('€435.48')).toBeInTheDocument();
    expect(screen.getByText('100%')).toBeInTheDocument();
    expect(screen.getByText('€1,392.57')).toBeInTheDocument();
    // a local regulation links to the regulation itself
    expect(screen.getByText(/art. 2.1 Kwijtscheldingsregeling/).closest('a')).toHaveAttribute('href', hhnk.url);
    expect(screen.getByText('Uitvoeringsregeling Invorderingswet 1990').closest('a')).toHaveAttribute('href', uri.url);
  });
});

describe('articleUrl', () => {
  it('links to the article of the version used', () => {
    expect(articleUrl(bijstand.evaluation, 'participatiewet', '31.2.d')).toBe('https://wetten.overheid.nl/BWBR0015703/2026-07-01#Artikel31');
    expect(articleUrl(bijstand.evaluation, 'unknown', '1')).toBeUndefined();
  });
});
