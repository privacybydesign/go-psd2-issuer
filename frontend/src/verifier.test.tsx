import { describe, it, expect, beforeAll } from 'vitest';
import { render, screen, within } from '@testing-library/react';
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
      criteria: [
        { key: 'income_norm', status: 'met', law: 'participatiewet', article: '19.1', amount: 80000, limit: 101390 },
        { key: 'municipality', status: 'met', law: 'participatiewet', article: '40', value: '6525 EC' },
        { key: 'household', status: 'not_checked', law: 'participatiewet', article: '3' },
        { key: 'assets', status: 'not_checked', law: 'participatiewet', article: '34' },
      ],
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
      criteria: [
        { key: 'payment_capacity', status: 'partial', law: uri.id, article: '13.1', amount: 14506, limit: 141946 },
        { key: 'health_premium', status: 'counted', law: uri.id, article: '15.1.c', amount: 3262 },
        { key: 'kwijtschelding_assets', status: 'not_checked', law: uri.id, article: '12' },
      ],
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

  it('shows the verdict and which criteria are checked and met', () => {
    render(<VerifierResultView result={bijstand} />);
    expect(screen.getByRole('heading', { name: 'You may be entitled to social assistance' })).toBeInTheDocument();
    expect(screen.getByText('Indicative supplement: €213.90 per month')).toBeInTheDocument();
    expect(screen.getByText(/You meet the 2 conditions we checked\. .* still assesses the 2 conditions we could not check/)).toBeInTheDocument();

    const checked = within(screen.getByRole('heading', { name: 'What we checked' }).closest('section')!);
    expect(checked.getByText('Income below the social assistance norm')).toBeInTheDocument();
    expect(checked.getAllByText('Met')).toHaveLength(2);
    // where you live, from the postcode on the address card
    expect(checked.getByText('Living in this municipality')).toBeInTheDocument();
    expect(checked.getByText(/Your postcode according to your address details: 6525 EC/)).toBeInTheDocument();
    expect(checked.getByText(/Your income is €800.00 per month; the norm for your situation is €1,013.90/)).toBeInTheDocument();

    const unchecked = within(screen.getByRole('heading', { name: 'What we did not check' }).closest('section')!);
    expect(unchecked.getByText('Assets below the asset limit')).toBeInTheDocument();
    expect(unchecked.getAllByText('Not checked')).toHaveLength(2);
    // the household is declared, not checked
    expect(unchecked.getByText(/You entered: no partner, 1 housemate/)).toBeInTheDocument();
    expect(unchecked.getByText('art. 34 Participatiewet').closest('a')).toHaveAttribute('href', 'https://wetten.overheid.nl/BWBR0015703/2026-07-01#Artikel34');

    // the municipality can make exceptions
    expect(screen.getByText(/very urgent reasons \(art. 16\)/)).toBeInTheDocument();
  });

  it('says clearly when someone is not entitled', () => {
    const result: VerifierResult = {
      ...bijstand,
      evaluation: {
        ...bijstand.evaluation,
        result: { ...bijstand.evaluation.result, outcome: 'no_indication', criteria: [{ ...bijstand.evaluation.result.criteria[0], status: 'not_met' }] },
      },
    };
    render(<VerifierResultView result={result} />);
    expect(screen.getByRole('heading', { name: 'You are probably not entitled to social assistance' })).toBeInTheDocument();
    expect(screen.getByText('Not met')).toBeInTheDocument();
    expect(screen.queryByText(/Indicative supplement:/)).not.toBeInTheDocument();
  });

  it('shows the calculation and what is not counted', () => {
    render(<VerifierResultView result={bijstand} />);
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
    expect(screen.getByRole('heading', { name: 'You may be eligible for partial remission' })).toBeInTheDocument();
    expect(screen.getByText('Partly met')).toBeInTheDocument();
    // a cost that is taken into account is no condition
    expect(screen.getByText('Counted')).toBeInTheDocument();
    expect(screen.getByText(/Counted: €32.62 per month/)).toBeInTheDocument();
    expect(screen.getByText(/You meet the condition we checked/)).toBeInTheDocument();
    expect(screen.getByText('You pay at most €1,392.57 per year')).toBeInTheDocument();
    expect(screen.getByText('Housing costs')).toBeInTheDocument();
    expect(screen.getByText('€435.48')).toBeInTheDocument();
    expect(screen.getByText('100%')).toBeInTheDocument();
    expect(screen.getAllByText('€1,392.57')).not.toHaveLength(0);
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
